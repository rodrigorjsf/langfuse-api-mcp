#!/usr/bin/env python3
#
# WHAT: offline unit tests of scripts/small-model-eval.py: the system prompt
#       names the run date, and the intent-11 matcher accepts a metrics query
#       only when its time window is the 7 days before that date (#105).
# WHY:  a model without a clock copied the fixed dates of the metrics query
#       example, and the matcher ignored timestamps, so intent 11 passed on
#       the one day those dates happened to be "the last 7 days". These cases
#       need no model, no network and no server.
# WHEN: by hand after changing the eval's system prompt or its matchers, and
#       before recording an eval run.
# HOW:  python3 scripts/test_small_model_eval.py   (standard library only)

import datetime
import importlib.util
import json
import os
import sys
import unittest

sys.dont_write_bytecode = True  # no scripts/__pycache__ left behind

_spec = importlib.util.spec_from_file_location(
    "small_model_eval", os.path.join(os.path.dirname(os.path.abspath(__file__)), "small-model-eval.py"))
evalmod = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(evalmod)

# The expected call of intent 11, written here as a literal so the test does
# not agree with the script by construction.
WANT = {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"],
        "granularity": "day", "last_days": 7}


def query(from_ts, to_ts):
    q = {"view": "observations", "metrics": [{"measure": "totalCost", "aggregation": "sum"}],
         "filters": [{"column": "traceName", "operator": "=", "value": "checkout", "type": "string"}],
         "timeDimension": {"granularity": "day"}}
    if from_ts is not None:
        q["fromTimestamp"] = from_ts
    if to_ts is not None:
        q["toTimestamp"] = to_ts
    return json.dumps(q)


class SystemPromptTest(unittest.TestCase):
    def test_the_system_prompt_names_the_run_date(self):
        prompt = evalmod.system_prompt(datetime.date(2026, 10, 15))
        self.assertIn("Today is 2026-10-15 (UTC).", prompt)


class MetricsWindowTest(unittest.TestCase):
    def matches(self, raw, today):
        return evalmod.metrics_query_matches(raw, WANT, today)

    def test_the_old_example_window_is_refused_on_any_other_run_date(self):
        old = query("2026-09-20T00:00:00Z", "2026-09-27T00:00:00Z")
        self.assertFalse(self.matches(old, datetime.date(2026, 10, 15)))
        self.assertFalse(self.matches(old, datetime.date(2026, 9, 28)))

    def test_the_old_example_window_is_accepted_on_the_day_it_was_the_last_7_days(self):
        old = query("2026-09-20T00:00:00Z", "2026-09-27T00:00:00Z")
        self.assertTrue(self.matches(old, datetime.date(2026, 9, 27)))

    def test_the_current_example_window_is_refused(self):
        example = query("2025-01-01T00:00:00Z", "2025-01-08T00:00:00Z")
        self.assertFalse(self.matches(example, datetime.date(2026, 10, 15)))

    def test_windows_that_are_not_the_last_7_days_are_refused(self):
        today = datetime.date(2026, 10, 15)
        for name, from_ts, to_ts in [
            ("starts 8 days back", "2026-10-07T00:00:00Z", "2026-10-15T00:00:00Z"),
            ("starts yesterday", "2026-10-14T00:00:00Z", "2026-10-15T00:00:00Z"),
            ("ends after the run date", "2026-10-08T00:00:00Z", "2026-10-16T00:00:01Z"),
            ("ends days before the run date", "2026-10-08T00:00:00Z", "2026-10-12T00:00:00Z"),
            ("no fromTimestamp", None, "2026-10-15T00:00:00Z"),
            ("fromTimestamp is not a date-time", "last week", "2026-10-15T00:00:00Z"),
            ("fromTimestamp is a number", 1760486400, "2026-10-15T00:00:00Z"),
            ("toTimestamp is not a date-time", "2026-10-08T00:00:00Z", "now"),
        ]:
            with self.subTest(name):
                self.assertFalse(self.matches(query(from_ts, to_ts), today))

    def test_the_last_7_days_before_the_run_date_are_accepted(self):
        today = datetime.date(2026, 10, 15)
        for name, from_ts, to_ts in [
            ("7 whole days to the start of today", "2026-10-08T00:00:00Z", "2026-10-15T00:00:00Z"),
            ("7 days including today", "2026-10-09T00:00:00Z", "2026-10-15T23:59:59Z"),
            ("to the end of today", "2026-10-08T00:00:00Z", "2026-10-16T00:00:00Z"),
            ("with milliseconds", "2026-10-08T00:00:00.000Z", "2026-10-15T00:00:00.000Z"),
            ("with an offset", "2026-10-08T02:00:00+02:00", "2026-10-15T00:00:00+00:00"),
            ("without toTimestamp", "2026-10-08T00:00:00Z", None),
        ]:
            with self.subTest(name):
                self.assertTrue(self.matches(query(from_ts, to_ts), today))

    def test_the_right_window_with_the_wrong_shape_is_still_refused(self):
        today = datetime.date(2026, 10, 15)
        wrong = json.loads(query("2026-10-08T00:00:00Z", "2026-10-15T00:00:00Z"))
        wrong["timeDimension"] = {"granularity": "week"}
        self.assertFalse(self.matches(json.dumps(wrong), today))


if __name__ == "__main__":
    unittest.main()
