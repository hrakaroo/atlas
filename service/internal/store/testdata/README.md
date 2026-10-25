# Frozen schema reference

python-schema-reference.json contains only the two DDL scripts extracted from the retired Python Library source, plus its source hash. It contains no live database or user material. Default Go tests compare embedded schema/relationship SQL to this independent historical reference; editing it requires explicit compatibility review. It permits checkout validation without the ignored old/ archive.

The optional atlas_python_reference build tag retains the original live Python endpoint/schema handoff test when that local archive is available.
