#!/usr/bin/env python3
#
# WHAT: unit tests of scripts/gen-union-catalog.py on literal fixture specs: a
#       write operation keeps the JSON request body of its own spec, and a
#       cyclic, malformed or foreign $ref stops generation instead of looping
#       or emitting a partial schema (#81); the body schema leaves the
#       generator in JSON Schema 2020-12, and a 3.0 keyword with no conversion
#       stops generation (#111).
# WHY:  pull-request CI proves the generator's committed output (internal/catalog
#       tests), not how it treats a hostile or broken release spec; these cases
#       need no network.
# WHEN: on every push and pull request (.github/workflows/ci.yml, job
#       "generator", #89), before each weekly regeneration
#       (.github/workflows/union-catalog.yml), and by hand after changing the
#       generator.
# HOW:  python3 scripts/test_gen_union_catalog.py   (needs PyYAML, like the generator:
#       pip install --require-hashes -r scripts/requirements.txt)

import importlib.util
import os
import sys
import unittest

sys.dont_write_bytecode = True  # no scripts/__pycache__ left behind

_spec = importlib.util.spec_from_file_location(
    "gen_union_catalog", os.path.join(os.path.dirname(os.path.abspath(__file__)), "gen-union-catalog.py"))
gen = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gen)


def spec_with_body(body, schemas):
    return {
        "paths": {"/api/public/x": {"post": {"operationId": "x_create", "requestBody": body}}},
        "components": {"schemas": schemas},
    }


def json_body(schema):
    return {"required": True, "content": {"application/json": {"schema": schema}}}


class RequestBodyTest(unittest.TestCase):
    def test_a_write_operation_keeps_its_body_with_refs_inlined_from_its_own_spec(self):
        spec = spec_with_body(json_body({"$ref": "#/components/schemas/CreateX"}), {
            "CreateX": {"type": "object", "required": ["name"],
                        "properties": {"name": {"type": "string"}, "kind": {"$ref": "#/components/schemas/Kind"}}},
            "Kind": {"type": "string", "enum": ["a", "b"]},
        })
        op = gen.operations(spec)["POST /api/public/x"]
        self.assertEqual(op["requestBody"], {"required": True, "content": {"application/json": {"schema": {
            "type": "object", "required": ["name"],
            "properties": {"name": {"type": "string"}, "kind": {"type": "string", "enum": ["a", "b"]}},
        }}}})

    def test_an_operation_without_a_body_gets_none(self):
        op = gen.operations({"paths": {"/api/public/x": {"get": {"operationId": "x_get"}}}})["GET /api/public/x"]
        self.assertNotIn("requestBody", op)

    def test_a_recursive_schema_stops_generation(self):
        spec = spec_with_body(json_body({"$ref": "#/components/schemas/Node"}), {
            "Node": {"type": "object", "properties": {"children": {
                "type": "array", "items": {"$ref": "#/components/schemas/Node"}}}},
        })
        with self.assertRaises(SystemExit) as stop:
            gen.operations(spec)
        self.assertIn("#/components/schemas/Node", str(stop.exception.code))

    def test_a_malformed_or_foreign_ref_stops_generation(self):
        for ref in ["#/components/schemas/Missing", "#/components/schemas/Leaf/deeper",
                    "https://evil.example/schema.json", "#/paths/~1api~1public~1x"]:
            with self.subTest(ref=ref), self.assertRaises(SystemExit) as stop:
                gen.operations(spec_with_body(json_body({"$ref": ref}), {"Leaf": "not a schema"}))
            self.assertIn(ref, str(stop.exception.code))

    def test_a_body_that_is_not_a_single_json_schema_stops_generation(self):
        for body in [{"content": {"text/plain": {"schema": {"type": "string"}}}},
                     {"content": {"application/json": {"schema": {}}, "text/plain": {"schema": {}}}},
                     {"content": {"application/json": {}}},
                     {"$ref": "#/components/schemas/Leaf"}]:
            with self.subTest(body=body), self.assertRaises(SystemExit) as stop:
                gen.operations(spec_with_body(body, {"Leaf": {"type": "string"}}))
            self.assertIn("POST /api/public/x", str(stop.exception.code))


def body_schema(schema, schemas=None):
    """The body schema the generator emits for an operation whose body is schema."""
    op = gen.operations(spec_with_body(json_body(schema), schemas or {}))["POST /api/public/x"]
    body = gen.body_2020_12("POST /api/public/x", op["requestBody"])
    return body["content"]["application/json"]["schema"]


class Dialect202012Test(unittest.TestCase):
    """#111: a body schema leaves the generator in JSON Schema 2020-12, not in
    the OpenAPI 3.0 dialect of the release specs."""

    def test_nullable_on_a_typed_property_becomes_a_type_union_with_null(self):
        got = body_schema({"type": "object", "properties": {
            "name": {"type": "string", "nullable": True, "description": "The name"}}})
        self.assertEqual(got, {"type": "object", "properties": {
            "name": {"type": ["string", "null"], "description": "The name"}}})

    def test_a_nullable_enum_also_lists_null_among_its_values(self):
        got = body_schema({"type": "string", "enum": ["a", "b"], "nullable": True})
        self.assertEqual(got, {"type": ["string", "null"], "enum": ["a", "b", None]})

    def test_nullable_on_an_untyped_property_that_accepts_anything_is_dropped(self):
        got = body_schema({"type": "object", "properties": {"input": {"nullable": True, "description": "Any"}}})
        self.assertEqual(got, {"type": "object", "properties": {"input": {"description": "Any"}}})

    def test_nullable_on_an_untyped_combinator_adds_null_as_an_alternative(self):
        got = body_schema({"nullable": True, "description": "Usage", "oneOf": [
            {"type": "object", "properties": {"n": {"type": "integer"}}}, {"type": "string"}]})
        self.assertEqual(got, {"description": "Usage", "anyOf": [
            {"oneOf": [{"type": "object", "properties": {"n": {"type": "integer"}}}, {"type": "string"}]},
            {"type": "null"}]})

    def test_a_nullable_const_adds_null_as_an_alternative(self):
        got = body_schema({"type": "string", "const": "x", "nullable": True, "title": "X"})
        self.assertEqual(got, {"title": "X", "anyOf": [{"type": "string", "const": "x"}, {"type": "null"}]})

    def test_nullable_is_converted_inside_combinators_items_and_additional_properties(self):
        got = body_schema({"$ref": "#/components/schemas/Req"}, {
            "Req": {"allOf": [{"type": "object", "properties": {
                "tags": {"type": "array", "nullable": True, "items": {"type": "string", "nullable": True}},
                "costs": {"type": "object", "additionalProperties": {"type": "number", "nullable": True}},
                "value": {"oneOf": [{"type": "integer", "nullable": True}, {"$ref": "#/components/schemas/S"}]},
            }}]},
            "S": {"type": "string", "nullable": True},
        })
        self.assertEqual(got, {"allOf": [{"type": "object", "properties": {
            "tags": {"type": ["array", "null"], "items": {"type": ["string", "null"]}},
            "costs": {"type": "object", "additionalProperties": {"type": ["number", "null"]}},
            "value": {"oneOf": [{"type": ["integer", "null"]}, {"type": ["string", "null"]}]},
        }}]})

    def test_a_property_or_value_named_nullable_is_data_and_stays(self):
        got = body_schema({"type": "object", "required": ["nullable"], "properties": {
            "nullable": {"type": "string", "enum": ["nullable"], "default": {"nullable": True}}}})
        self.assertEqual(got, {"type": "object", "required": ["nullable"], "properties": {
            "nullable": {"type": "string", "enum": ["nullable"], "default": {"nullable": True}}}})

    def test_a_boolean_exclusive_bound_becomes_the_numeric_bound(self):
        got = body_schema({"type": "object", "properties": {
            "p": {"type": "number", "minimum": 0, "exclusiveMinimum": True, "maximum": 1, "exclusiveMaximum": True},
            "q": {"type": "integer", "minimum": 1, "exclusiveMinimum": False, "maximum": 9, "exclusiveMaximum": False}}})
        self.assertEqual(got, {"type": "object", "properties": {
            "p": {"type": "number", "exclusiveMinimum": 0, "exclusiveMaximum": 1},
            "q": {"type": "integer", "minimum": 1, "maximum": 9}}})

    def test_a_boolean_exclusive_bound_without_its_bound_stops_generation(self):
        for schema in [{"type": "number", "exclusiveMinimum": True}, {"type": "number", "exclusiveMaximum": True}]:
            with self.subTest(schema=schema), self.assertRaises(SystemExit) as stop:
                body_schema(schema)
            self.assertIn("POST /api/public/x", str(stop.exception.code))

    def test_an_openapi_only_keyword_with_no_conversion_stops_generation(self):
        for keyword in ["discriminator", "example", "xml", "externalDocs"]:
            with self.subTest(keyword=keyword), self.assertRaises(SystemExit) as stop:
                body_schema({"type": "object", "properties": {"p": {"type": "string", keyword: {}}}})
            self.assertIn("POST /api/public/x", str(stop.exception.code))
            self.assertIn(keyword, str(stop.exception.code))

    def test_a_nullable_typed_schema_with_a_combinator_adds_null_as_an_alternative(self):
        got = body_schema({"type": "object", "nullable": True, "allOf": [{"type": "object", "required": ["a"]}]})
        self.assertEqual(got, {"anyOf": [{"type": "object", "allOf": [{"type": "object", "required": ["a"]}]},
                                         {"type": "null"}]})


if __name__ == "__main__":
    unittest.main()
