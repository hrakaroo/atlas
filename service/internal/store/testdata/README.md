# Frozen schema reference

`python-schema-reference.json` contains two code-only DDL scripts and a provenance hash. It contains no live database or user material. Go tests compare embedded schema and relationship SQL to this independent reference. Changes require an explicit compatibility review rather than regenerating the fixture to match implementation changes.

The fixture's filename is retained for stable test references. Default checkout validation uses only the checked-in schema definitions.
