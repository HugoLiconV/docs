---
title: Spatial Indexes
summary: How CockroachDB uses spatial indexes for efficiently storing and querying spatial data.
toc: true
---

{% include {{page.version.version}}/sql/spatial-support-new.md %}

This page describes CockroachDB's approach to indexing spatial data, including:

- What spatial indexing is
- How it works

## What is a spatial index?

A spatial index is just like any other [index](indexes.html).  Its purpose in life is to improve your database's performance by helping SQL locate data without having to look through every row of a table.

Spatial indexes are used for the same tasks as any other index type:

- Fast filtering of lists of objects based on spatial predicate functions such as `ST_Contains`.

- Speeding up joins between spatial and non-spatial data.

They differ from other types of indexes as follows:

- Their inner workings are specialized to operate on 2-dimensional `GEOMETRY` and `GEOGRAPHY` data types.  They are stored by CockroachDB as a special type of [inverted index](inverted-indexes.html).  For more details, see [Index storage](#index-storage) below.

- They can be tuned to store looser or tighter coverings of the shapes being indexed, depending on the needs of your application.  Tighter coverings are more expensive to generate and store (and update), but perform better because they return fewer false positives during the initial index lookup.  For more information, see [Tuning spatial indexes](#tuning-spatial-indexes) below.

## How CockroachDB's spatial indexing works

### Overview

There are two main approaches to building geospatial indexes:

- One approach is to "divide the objects". This works by inserting the objects into a tree (usually a balanced tree such as an [R-tree](https://en.wikipedia.org/wiki/R-tree)) whose shape depends on the data being indexed.

- The other approach is to "divide the space". This works by creating a decomposition of the space being indexed into buckets of various sizes.

Whichever approach to indexing is used, when an object is indexed, a "covering" shape (e.g. a bounding box) is constructed that completely encompasses the indexed object. Index queries work by looking for containment or intersection between the covering shape for the query object and the indexed covering shapes. This retrieves false positives but no false negatives.

CockroachDB takes the "divide the space" approach to spatial indexing.  This is necessary to preserve CockroachDB's ability to [scale horizontally](frequently-asked-questions.html#how-does-cockroachdb-scale) by [adding nodes to a running cluster](cockroach-start.html#add-a-node-to-a-cluster).

Advantages of the divide the space approach include:

+ Easy to scale horizontally.
+ No balancing operations are required, unlike [R-tree indexes](https://en.wikipedia.org/wiki/R-tree).
+ Inserts require no locking.
+ Bulk ingest is simpler to implement than other approaches.
+ Allows a per-object tradeoff between index size and false positives during index creation.  (See [Tuning spatial indexes](#tuning-spatial-indexes) below.)

Disadvantages of "divide the space" include:

+ It does not support indexing infinite `GEOMETRY` types. Because the space is divided beforehand, it must be finite. This means that CockroachDB's spatial indexing works for `GEOGRAPHY` (spherical) and for finite `GEOMETRY` (planar) objects, but not for infinite `GEOMETRY`.
+ Includes more false positives in the index by default, which must then be filtered out by the SQL execution layer.  This filtering can reduce performance, and thus [tuning spatial indexes](#tuning-spatial-indexes) becomes more important to get good performance.

### Details

Under the hood, CockroachDB uses the [S2 geometry library](https://s2geometry.io/) to divide the space being indexed into a [quadtree](https://en.wikipedia.org/wiki/Quadtree) data structure with a set number of levels and a data-independent shape. Each node in the quad tree (really, [S2 cell](https://s2geometry.io/devguide/s2cell_hierarchy.html)) represents some part of the indexed space and is divided once horizontally and once vertically to produce 4 child cells in the next level.

The leaf nodes of the quadtree measure 1cm across the Earth's surface.  This means that, depending on the shapes you are working with, you can tune the spatial accuracy of your indexes down to 1cm (with tradeoffs -- see [Tuning spatial indexes](#tuning-spatial-indexes) below).

The nodes in the tree are numbered using a [Hilbert space-filling curve](https://en.wikipedia.org/wiki/Hilbert_curve) which preserves locality of reference.  In other words, two points that are near each other geometrically are likely to be near each other in the quadtree, which is good for performance.

Visually, you can think of the S2 library as enclosing a sphere in a cube as shown in the image below.  We map from points on each face of the cube to points on the face of the sphere.  As you can see in the picture below, there is a projection that occurs in this mapping: the lines entering from the left are "refracted" by the material of the cube face before touching the surface of the sphere.  This projection reduces the distortion that would occur if the points on the cube face were projected directly onto the sphere in a straight line.

<img style="display: block; margin-left: auto; margin-right: auto; width: 50%" src="{{ 'images/v20.2/geospatial/s2-cubed-sphere-2d.png' | relative_url }}" alt="S2 Cubed Sphere - 2D">

Next, let's expand the image to 3 dimensions, to show the cube and sphere more clearly.  As mentioned above, each cube face is mapped to the quadtree data structure, and each node of the quadtree is numbered using a Hilbert space-filling curve.  In the image below, you can imagine how the points on the Hilbert curve on the rear face of the cube are projected onto the sphere in the center.

<img style="display: block; margin-left: auto; margin-right: auto; width: 50%" src="{{ 'images/v20.2/geospatial/s2-cubed-sphere-3d.png' | relative_url }}" alt="S2 Cubed Sphere - 3D">

When you index a spatial object, a covering is computed using some number of the cells in the quadtree. The number of covering cells can vary per indexed object by passing special arguments to `CREATE INDEX` that tell CockroachDB how many levels of s2 cells to use.  For more information about these tuning parameters, see [Tuning spatial indexes](#tuning-spatial-indexes).

## Tuning spatial indexes

When an object is indexed, a "covering" shape (e.g. a bounding box) is constructed that completely encompasses the indexed object. Index queries work by looking for containment or intersection between the covering shape for the query object and the indexed covering shapes. This retrieves false positives but no false negatives.

This leads to an important tradeoff when creating spatial indexes.  The number of cells used to represent an object in the index is tunable fewer cells use less space but create a looser covering. A looser covering retrieves more false positives from the index, which is expensive because the exact answer computation that's run after the index query is expensive. However, at some point the benefits of retrieving fewer false positives is outweighed by how long it takes to scan a large index.

The size of the large index that is created for a tighter covering also matters if the table is accepting a lot of writes.  If the table is accepting more frequent writes, the (larger) index will need to be updated more frequently.

Let's look at a concrete example.

The following geometry object describes a `LINESTRING` whose vertices land on some small cities in the Northeastern US:

~~~
'LINESTRING(-76.4955 42.4405,  -75.6608 41.4102,-73.5422 41.052, -73.929 41.707, -76.4955 42.4405)'
~~~

The animated image below shows the s2 covering that is generated as we "turn up the dial" on the `s2_max_level` and `s2_max_cells` parameters, iterating them up from 1 to 30:

<img style="display: block; margin-left: auto; margin-right: auto; width: 50%" src="{{ 'images/v20.2/geospatial/s2-coverings.gif' | relative_url }}" alt="Animated GIF of S2 Coverings - Levels 1 to 30">

Here are the same images, presented in a grid.  As we turn up the `s2_max_cells` parameter, more work is done by CockroachDB to discover a tighter and tighter covering (that is, a covering using fewer and smaller cells).  Note that this means that the resulting indexes grow larger and larger.

<img style="display: block; margin-left: auto; margin-right: auto; width: 50%" src="{{ 'images/v20.2/geospatial/s2-coverings-tiled.png' | relative_url }}" alt="Static image of S2 Coverings - Levels 1 to 30">

The following keyword options are supported for both `CREATE INDEX` and the [built-in function](functions-and-operators.html#geospatial-functions) `st_s2covering`.  The latter is useful for seeing what kinds of coverings would be generated in an index if you created an index with various tuning parameters set.

| Option         | Default value | Meaning                                                                                                                                                                |
|----------------+---------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| s2_level_mod   |             1 |                                                                                                                                                                        |
| s2_max_level   |            30 |                                                                                                                                                                        |
| s2_max_cells   |             4 | A limit on how much work is done exploring the possible covering.  Defaults to 8.  You may want to use higher values for odd-shaped regions such as skinny rectangles. |
| geometry_min_x |               |                                                                                                                                                                        |
| geometry_max_x |               |                                                                                                                                                                        |
| geometry_min_y |               |                                                                                                                                                                        |
| geometry_max_y |               |                                                                                                                                                                        |

### Example - tuning index creation

Here is an example showing all of the options being set on `CREATE INDEX`:

{% include copy-clipboard.html %}
~~~ sql
CREATE INVERTED INDEX geom_idx_2
	ON some_spatial_table (geom)
	WITH (
		s2_max_cells = 20, s2_max_level = 12, s2_level_mod = 3, geometry_min_x = -180, geometry_max_x = 180, geometry_min_y = -180, geometry_max_y = 180
	)
~~~

### Example - viewing an object's s2 covering

Here is an example showing how to pass the options to `st_s2covering`.  It generates [GeoJSON](https://geojson.org) output showing both a shape and the s2 covering that would be generated for that shape in your index, if you passed the same parameters to `CREATE INDEX`.  You can paste this output into [geojson.io](http://geojson.io) to see what it looks like.

{% include copy-clipboard.html %}
~~~ sql
CREATE TABLE tmp (id INT8, geom GEOMETRY);

INSERT
INTO
	tmp (id, geom)
VALUES
	(
		1,
		st_geomfromtext(
			'LINESTRING(-76.8261 42.1727,  -75.6608 41.4102,-73.5422 41.052, -73.929 41.707,  -76.8261 42.1727)'
		)
	);

INSERT
INTO
	tmp (id, geom)
VALUES
	(
		2,
		st_s2covering(
			st_geomfromtext(
				'LINESTRING(-76.8261 42.1727,  -75.6608 41.4102,-73.5422 41.052, -73.929 41.707,  -76.8261 42.1727)'
			),
			's2_max_cells=20,s2_max_level=12,s2_level_mod=3,geometry_min_x=-180,geometry_max_x=180,geometry_min_y=-180,geometry_max_y=180'
		)
	);

SELECT st_asgeojson(st_collect(geom)) FROM tmp;
~~~

{% include copy-clipboard.html %}
~~~ json
{"type":"GeometryCollection","geometries":[{"type":"LineString","coordinates":[[-76.8261,42.1727],[-75.6608,41.4102],[-73.5422,41.052],[-73.929,41.707],[-76.8261,42.1727]]},{"type":"MultiPolygon","coordinates":[[[[-76.904296875,42.099609375],[-76.81640625,42.099609375],[-76.81640625,42.1875],[-76.904296875,42.1875],[-76.904296875,42.099609375]]],[[[-76.81640625,42.099609375],[-76.728515625,42.099609375],[-76.728515625,42.1875],[-76.81640625,42.1875],[-76.81640625,42.099609375]]],[[[-76.728515625,42.099609375],[-76.640625,42.099609375],[-76.640625,42.1875],[-76.728515625,42.1875],[-76.728515625,42.099609375]]],[[[-76.728515625,42.01171875],[-76.640625,42.01171875],[-76.640625,42.099609375],[-76.728515625,42.099609375],[-76.728515625,42.01171875]]],[[[-76.640625,41.484375],[-75.9375,41.484375],[-75.9375,42.1875],[-76.640625,42.1875],[-76.640625,41.484375]]],[[[-74.53125,40.78125],[-73.828125,40.78125],[-73.828125,41.484375],[-74.53125,41.484375],[-74.53125,40.78125]]],[[[-73.828125,40.78125],[-73.125,40.78125],[-73.125,41.484375],[-73.828125,41.484375],[-73.828125,40.78125]]],[[[-73.828125,41.484375],[-73.740234375,41.484375],[-73.740234375,41.572265625],[-73.828125,41.572265625],[-73.828125,41.484375]]],[[[-74.53125,41.484375],[-73.828125,41.484375],[-73.828125,42.1875],[-74.53125,42.1875],[-74.53125,41.484375]]],[[[-75.234375,41.484375],[-74.53125,41.484375],[-74.53125,42.1875],[-75.234375,42.1875],[-75.234375,41.484375]]],[[[-75.234375,40.78125],[-74.53125,40.78125],[-74.53125,41.484375],[-75.234375,41.484375],[-75.234375,40.78125]]],[[[-75.322265625,41.30859375],[-75.234375,41.30859375],[-75.234375,41.396484375],[-75.322265625,41.396484375],[-75.322265625,41.30859375]]],[[[-75.41015625,41.30859375],[-75.322265625,41.30859375],[-75.322265625,41.396484375],[-75.41015625,41.396484375],[-75.41015625,41.30859375]]],[[[-75.5859375,41.396484375],[-75.498046875,41.396484375],[-75.498046875,41.484375],[-75.5859375,41.484375],[-75.5859375,41.396484375]]],[[[-75.5859375,41.30859375],[-75.498046875,41.30859375],[-75.498046875,41.396484375],[-75.5859375,41.396484375],[-75.5859375,41.30859375]]],[[[-75.498046875,41.30859375],[-75.41015625,41.30859375],[-75.41015625,41.396484375],[-75.498046875,41.396484375],[-75.498046875,41.30859375]]],[[[-75.673828125,41.396484375],[-75.5859375,41.396484375],[-75.5859375,41.484375],[-75.673828125,41.484375],[-75.673828125,41.396484375]]],[[[-75.76171875,41.396484375],[-75.673828125,41.396484375],[-75.673828125,41.484375],[-75.76171875,41.484375],[-75.76171875,41.396484375]]],[[[-75.849609375,41.396484375],[-75.76171875,41.396484375],[-75.76171875,41.484375],[-75.849609375,41.484375],[-75.849609375,41.396484375]]],[[[-75.9375,41.484375],[-75.234375,41.484375],[-75.234375,42.1875],[-75.9375,42.1875],[-75.9375,41.484375]]]]}]}
~~~

When you paste this output into [geojson.io](http://geojson.io), it generates the picture below, which shows both the `LINESTRING` and its S2 covering based on the options you passed to `st_s2covering`.

<img style="display: block; margin-left: auto; margin-right: auto; width: 50%" src="{{ 'images/v20.2/geospatial/s2-linestring-example-covering.png' | relative_url }}" alt="S2 LINESTRING example covering">

## Index storage

CockroachDB stores spatial indexes as a special type of [inverted index](inverted-indexes.html).  The spatial inverted index maps from a location to one or more shapes whose [coverings](spatial-glossary.html#covering) include that location.  Since a location can be used in the covering for multiple shapes, and each shape can have multiple locations in its covering, there is a many-to-many relationship between locations and shapes.

As such, a row in the index might look 

| Key                           | Value(s)                 |
|-------------------------------+--------------------------|
| 'POINT(-74.147896 41.679517)' | geom1, geom2, ...        |
| 'LINESTRING()'                | geom3, geom4, geom5, ... |

To control how many duplicates occur in the list of values (and thus how many false positives will be returned by an index lookup), you can configure the index to use more or fewer s2 cells with the `s2_max_level` and `s2_max_cells` arguments to `CREATE INDEX` (see [Spatial index tuning](#spatial-index-tuning) below).

{{site.data.alerts.callout_danger}}
CockroachDB does not support indexing geospatial types in default [primary keys](primary-key.html) and [unique secondary indexes](indexes.html#unique-secondary-indexes). This is because we will not be able to match the PostGIS definition as it's based on a hash of its internal data structure, which means we will not be able to be a "drop-in" replacement here.
{{site.data.alerts.end}}

XXX: YOU ARE HERE

## Examples

### Create a `GEOGRAPHY` index

XXX: WRITE ME

### Create a `GEOMETRY` index

To create a spatial index on a `GEOMETRY` data type, enter the following statement:

{% include copy-clipboard.html %}
~~~ sql
CREATE INDEX geom_idx_1 ON some_spatial_table USING GIST(geom) WITH (s2_level_mod=3);
~~~

{% include copy-clipboard.html %}
~~~ sql
-- Taken from SQL logictests
CREATE INDEX geom_idx_1 ON geo_table USING GIST(geom) WITH (geometry_min_x=0, s2_max_level=15)
CREATE INDEX geom_idx_3 ON geo_table USING GIST(geom) WITH (s2_max_level=10)
CREATE INDEX geog_idx_1 ON geo_table USING GIST(geog) WITH (s2_level_mod=2)

CREATE INDEX geom_idx_1 ON geo_table USING GIST(geom) WITH (geometry_min_x=0, s2_max_level=15);
CREATE INDEX geog_idx_1 ON geo_table USING GIST(geog) WITH (s2_level_mod=3);

CREATE TABLE public.geo_table (
   id INT8 NOT NULL,
   geog GEOGRAPHY(GEOMETRY,4326) NULL,
   geom GEOMETRY(GEOMETRY,3857) NULL,
   CONSTRAINT "primary" PRIMARY KEY (id ASC),
   INVERTED INDEX geom_idx_1 (geom) WITH (s2_max_level=15, geometry_min_x=0),
   INVERTED INDEX geom_idx_2 (geom) WITH (geometry_min_x=0),
   INVERTED INDEX geom_idx_3 (geom) WITH (s2_max_level=10),
   INVERTED INDEX geom_idx_4 (geom),
   INVERTED INDEX geog_idx_1 (geog) WITH (s2_level_mod=2),
   INVERTED INDEX geog_idx_2 (geog),
   FAMILY fam_0_geog (geog),
   FAMILY fam_1_geom (geom),
   FAMILY fam_2_id (id)
)
~~~

To see the S2 Coverings:

{% include copy-clipboard.html %}
~~~ sql
ST_AsEWKT(ST_S2Covering(st_geomfromtext(''), 's2_max_cells=2'))
ST_AsEWKT(ST_S2Covering(geog::geography, 's2_max_cells=2'))
~~~

In many cases, you will need to constrain your index using the keywords XXX, YYY to get a useful s2 covering.

Here is an example of constraining the min and max values of the s2 cells to get a useful covering

{% include copy-clipboard.html %}
~~~ sql
SELECT st_asgeojson(st_s2covering(st_geomfromtext('LINESTRING(-76 42, -74 41, -73 40, -74 42, -76 42)'), 's2_max_cells=20,s2_max_level=12,s2_level_mod=3,geometry_min_x=-180,geometry_max_x=180,geometry_min_y=-180,geometry_max_y=180'));
~~~

~~~ json
{"type":"MultiPolygon","coordinates":[[[[-73.125,40.166015625],[-73.037109375,40.166015625],[-73.037109375,40.25390625],[-73.125,40.25390625],[-73.125,40.166015625]]],[[[-73.125,40.078125],[-73.037109375,40.078125],[-73.037109375,40.166015625],[-73.125,40.166015625],[-73.125,40.078125]]],[[[-73.037109375,39.990234375],[-72.94921875,39.990234375],[-72.94921875,40.078125],[-73.037109375,40.078125],[-73.037109375,39.990234375]]],[[[-73.125,39.990234375],[-73.037109375,39.990234375],[-73.037109375,40.078125],[-73.125,40.078125],[-73.125,39.990234375]]],[[[-76.025390625,41.923828125],[-75.9375,41.923828125],[-75.9375,42.01171875],[-76.025390625,42.01171875],[-76.025390625,41.923828125]]],[[[-73.828125,40.078125],[-73.125,40.078125],[-73.125,40.78125],[-73.828125,40.78125],[-73.828125,40.078125]]],[[[-74.53125,40.78125],[-73.828125,40.78125],[-73.828125,41.484375],[-74.53125,41.484375],[-74.53125,40.78125]]],[[[-73.828125,40.78125],[-73.125,40.78125],[-73.125,41.484375],[-73.828125,41.484375],[-73.828125,40.78125]]],[[[-73.828125,41.484375],[-73.740234375,41.484375],[-73.740234375,41.572265625],[-73.828125,41.572265625],[-73.828125,41.484375]]],[[[-73.828125,41.572265625],[-73.740234375,41.572265625],[-73.740234375,41.66015625],[-73.828125,41.66015625],[-73.828125,41.572265625]]],[[[-74.53125,41.484375],[-73.828125,41.484375],[-73.828125,42.1875],[-74.53125,42.1875],[-74.53125,41.484375]]],[[[-75.234375,41.484375],[-74.53125,41.484375],[-74.53125,42.1875],[-75.234375,42.1875],[-75.234375,41.484375]]],[[[-74.619140625,41.30859375],[-74.53125,41.30859375],[-74.53125,41.396484375],[-74.619140625,41.396484375],[-74.619140625,41.30859375]]],[[[-74.70703125,41.30859375],[-74.619140625,41.30859375],[-74.619140625,41.396484375],[-74.70703125,41.396484375],[-74.70703125,41.30859375]]],[[[-74.794921875,41.396484375],[-74.70703125,41.396484375],[-74.70703125,41.484375],[-74.794921875,41.484375],[-74.794921875,41.396484375]]],[[[-74.8828125,41.396484375],[-74.794921875,41.396484375],[-74.794921875,41.484375],[-74.8828125,41.484375],[-74.8828125,41.396484375]]],[[[-74.794921875,41.30859375],[-74.70703125,41.30859375],[-74.70703125,41.396484375],[-74.794921875,41.396484375],[-74.794921875,41.30859375]]],[[[-74.619140625,41.220703125],[-74.53125,41.220703125],[-74.53125,41.30859375],[-74.619140625,41.30859375],[-74.619140625,41.220703125]]],[[[-74.970703125,41.396484375],[-74.8828125,41.396484375],[-74.8828125,41.484375],[-74.970703125,41.484375],[-74.970703125,41.396484375]]],[[[-75.9375,41.484375],[-75.234375,41.484375],[-75.234375,42.1875],[-75.9375,42.1875],[-75.9375,41.484375]]]]}
~~~

## See also

- [Inverted Indexes](inverted-indexes.html)
- [S2 Geometry Library](https://s2geometry.io/)
- [Indexes](indexes.html)
- [Spatial Features](spatial-features.html)
- [Working with Spatial Data](spatial-data.html)
- [Spatial and GIS Glossary of Terms](spatial-glossary.html)
- [Spatial functions](functions-and-operators.html#geospatial-functions)
- [Migrate from Shapefiles](migrate-from-shapefiles.html)
- [Migrate from GeoJSON](migrate-from-geojson.html)
- [Migrate from GeoPackage](migrate-from-geopackage.html)
- [Migrate from OpenStreetMap](migrate-from-openstreetmap.html)
