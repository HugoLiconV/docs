---
title: SHOW SCHEMAS
summary: The SHOW SCHEMAS statement lists the schemas in a database.
toc: true
---

The `SHOW SCHEMAS` [statement](sql-statements.html) lists all [schemas](sql-name-resolution.html#logical-schemas-and-namespaces) in a database.

## Required privileges

No [privileges](authorization.html#assign-privileges) are required to list the schemas in a database.

## Synopsis

<div>
  {% include {{ page.version.version }}/sql/diagrams/show_schemas.html %}
</div>

## Parameters

Parameter | Description
----------|------------
`name` | The name of the database for which to show [schemas](sql-name-resolution.html#logical-schemas-and-namespaces). When omitted, the schemas in the [current database](sql-name-resolution.html#current-database) are listed.

## Example

{% include {{page.version.version}}/sql/movr-statements.md %}

### Show schemas in the current database

{% include copy-clipboard.html %}
~~~ sql
> CREATE SCHEMA org_one;
~~~

{% include copy-clipboard.html %}
~~~ sql
> SHOW SCHEMAS;
~~~

~~~
     schema_name     | owner
---------------------+--------
  crdb_internal      | NULL
  information_schema | NULL
  org_one            | demo
  pg_catalog         | NULL
  pg_extension       | NULL
  public             | admin
(6 rows)
~~~

## See also

- [Logical Schemas and Namespaces](sql-name-resolution.html)
- [`CREATE SCHEMA`](create-schema.html)
- [`SHOW DATABASES`](show-databases.html)
- [Information Schema](information-schema.html)
- [Other SQL Statements](sql-statements.html)
