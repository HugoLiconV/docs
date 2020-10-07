---
title: Spatial Indexes
summary: How CockroachDB uses spatial indexes for efficiently storing and querying spatial data.
toc: true
---

{% include {{page.version.version}}/sql/spatial-support-new.md %}

This page describes CockroachDB's approach to indexing spatial data, including:

- What spatial indexing is
- How spatial indexing works

## What is a spatial index?

A spatial index is just like any other [index](indexes.html).  Its purpose in life is to improve your database's performance by helping SQL locate data without having to look through every row of a table.

Spatial indexes are mainly used for the same tasks as any other index type, namely:

- Fast filtering of objects based on spatial predicate functions, such as `ST_Contains`.

- Speeding up joins between spatial and non-spatial data.

It differs from other indexes as follows:

- Its inner workings are specialized to operate on 2-dimensional `GEOMETRY` and `GEOGRAPHY` data types.

- It is stored by CockroachDB as a special type of [inverted index](inverted-indexes.html).  For more details, see [How CockroachDB's spatial indexing works](#how-cockroachdbs-spatial-indexing-works) below.

## How CockroachDB's spatial indexing works

At a high level, CockroachDB takes a "divide the space" approach to spatial indexing that works by decomposing the space being indexed into buckets of various sizes.  This approach is necessary to preserve CockroachDB's ability to scale horizontally.

CockroachDB uses the [S2 geometry library](https://s2geometry.io/) to divide the space being indexed into a [quadtree](https://en.wikipedia.org/wiki/Quadtree) data structure with a set number of levels and a data-independent shape. Each node in the quad tree (really, S2 cell) represents some part of the indexed space and is divided once horizontally and once vertically to produce 4 child cells in the next level. The nodes are numbered using a [Hilbert space-filling curve](https://en.wikipedia.org/wiki/Hilbert_curve) which preserves locality; the leaf nodes of the quadtree measure 1cm across the Earth's surface.  This means that spatial accuracy of your indexes is tunable down to 1cm (with tradeoffs of accuracy vs. speed during index creation -- see below).

This is easier to understand with pictures.  At a high level, we enclose the sphere into a cube.  Each face of the cube is a square.  We then map from points on that square to points on the face of the sphere.  As you can see in the picture below, there is a projection that occurs.  In the picture, the lines entering from the left are "refracted" by the material of the cube face and are projected onto the surface of the sphere.  This projection reduces the distortion that would occur if the points on the cube face were projected directly onto the sphere in a straight line.

<img style="display: block; margin-left: auto; margin-right: auto; width: 50%" src="{{ 'images/v20.2/geospatial/s2-cubed-sphere-2d.png' | relative_url }}" alt="S2 Cubed Sphere - 2D">

Next let's expand the image to 3 dimensions, to show the cube and sphere more clearly.  Above, we mentioned that each cube face is mapped to a quadtree data structure.  The nodes of each quadtree are numbered using a Hilbert space-filling curve.  In the image below, you can imagine that the points on the Hilbert curve on the rear face of the cube are projected onto the sphere in the center.

<img style="display: block; margin-left: auto; margin-right: auto; width: 50%" src="{{ 'images/v20.2/geospatial/s2-cubed-sphere-3d.png' | relative_url }}" alt="S2 Cubed Sphere - 3D">

When indexing an object, a covering is computed using some number of the cells in the quadtree. The number of covering cells can vary per indexed object by passing special arguments to [`CREATE INDEX`](create-index.html) that tell CockroachDB how many levels of s2 cells to use.

There is an important tradeoff in the number of cells used to represent an object in the index: fewer cells use less space but create a looser covering. A looser covering retrieves more false positives from the index, which is expensive because the exact answer computation that's run after the index query is expensive. However, at some point the benefits of retrieving fewer false positives is outweighed by how long it takes to scan a large index.

The size of a large index also matters if the table is accepting a lot of writes.



Advantages of CockroachDB's "divide the space" approach to spatial indexing include:

+ Easy to scale horizontally.
+ No balancing operations are required, unlike [R-tree indexes](https://en.wikipedia.org/wiki/R-tree).
+ Inserts require no locking.
+ Bulk ingest is simpler to implement than other approaches.
+ Allows a per-object tradeoff between index size and false positives during index creation.

Disadvantages of the "divide the space" approach include:

+ Does not support indexing infinite {{GEOMETRY}} types. Because the space is divided beforehand, it must be finite. This means that CockroachDB's spatial indexing works for (spherical) {{GEOGRAPHY}} and for finite {{GEOMETRY}} (planar) but not for infinite {{GEOMETRY}}.
+ Includes more false positives in the index by default, which must then be filtered out by the SQL execution layer.  This filtering can be slow, and thus tuning spatial indexes becomes more important to get good performance.

## Index storage

CockroachDB stores spatial indexes as a special type of [inverted index](inverted-indexes.html).  The spatial inverted index maps from a location to one or more shapes whose [coverings](spatial-glossary.html#covering) include that location.  Since a location can be used in the covering for multiple shapes, and each shape can have multiple locations in its covering, there is a many-to-many relationship between locations and shapes.

As such, a row in the index might look 

| Key                           | Value(s)                 |
|-------------------------------+--------------------------|
| 'POINT(-74.147896 41.679517)' | geom1, geom2, ...        |
| 'LINESTRING()'                | geom3, geom4, geom5, ... |

To control how many duplicates occur in the list of values, you can configure the index to use more or fewer s2 cells with the `s2_max_level` and `s2_max_cells` arguments to `CREATE INDEX` (see the examples below).

## Examples

### Create a `GEOGRAPHY` index

XXX: write this section

### Create a `GEOMETRY` index

To create a spatial index on a `GEOMETRY` data type, enter the following statement:

{% include copy-clipboard.html %}
~~~ sql
CREATE INDEX geom_idx_1 ON some_spatial_table USING GIST(geom) WITH (s2_level_mod=3);
~~~

### Spatial index options

The following keyword options to [`CREATE INDEX`](create-index.html) are supported:

| Option         | Value | Meaning |
|----------------+-------+---------|
| s2_level_mod   |     3 |         |
| s2_max_level   |    15 |         |
| s2_max_cells   |       |         |
| geometry_min_x |       |         |
| geometry_max_x |       |         |
| geometry_min_y |       |         |
| geometry_max_y |       |         |

Here is an example showing all of the options being set:

{% include copy-clipboard.html %}
~~~ sql
CREATE INDEX geom_idx_2 ON some_spatial_table USING GIST(geom) WITH ('s2_max_cells=20,s2_max_level=12,s2_level_mod=3,geometry_min_x=-180,geometry_max_x=180,geometry_min_y=-180,geometry_max_y=180');
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
