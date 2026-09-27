#!/usr/bin/env python3
#
# WHAT: unit tests of scripts/gen-union-catalog.py on literal fixture specs: a
#       write operation keeps the JSON request body of its own spec, and a
#       cyclic, malformed or foreign $ref stops generation instead of looping
#       or emitting a partial schema (#81).
# WHY:  pull-request CI proves the generator's committed output (internal/catalog
#       tests), not how it treats a hostile or broken release spec; these cases
#       need no network.
# WHEN: on every push and pull request (.github/workflows/ci.yml, job
#       "generator", #89), before each weekly regeneration
#       (.github/workflows/union-catalog.yml), and by hand after changing the
#       generator.
# HOW:  python3 scripts/test_gen_union_catalog.py   (needs PyYAML, like the generator)

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


if __name__ == "__main__":
    unittest.main()
