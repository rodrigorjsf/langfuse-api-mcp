#!/usr/bin/env python3
#
# WHAT: the bookkeeping of the end-to-end campaign
#       (docs/development/integration-tests.md "End-to-end campaign"), over the
#       audit files scripts/e2e-mcp-server.sh writes (dist/e2e/audit-ro.log and
#       audit-rw.log). Three subcommands:
#         mark <label>                 append the label, the UTC time and both
#                                      files' line counts to dist/e2e/ledger.tsv
#         count <ro|rw> <from> [to]    tool calls, calls with an error code,
#                                      execute_write calls and confirmations in
#                                      the audit lines after <from> up to <to>
#         call <ro|rw> <tool> <json|-> one tool call over stdio through the
#                                      wrapper (tool "tools/list" lists tools);
#                                      "-" reads the arguments from stdin
# WHY:  a worker's report says what it believes it called; the audit line says
#       what the server ran. Marking the line count before and after each
#       worker lets the calls be attributed, and `call` gives the orchestrator
#       a deterministic client for checks no agent should improvise (byte-level
#       scans, oversized bodies).
# WHEN: by hand or by the e2e-campaign skill, while the campaign runs.
# HOW:  python3 scripts/e2e-audit.py mark sweep-start
#       python3 scripts/e2e-audit.py count ro 28 126
#       python3 scripts/e2e-audit.py call ro execute_read '{"operationId":"health_health"}'
#       python3 scripts/e2e-audit.py --self-test
# Standard library only. Prints no key: the wrapper reads them itself.
import collections
import json
import os
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
E2E = os.path.join(ROOT, "dist", "e2e")


def lines(mode):
    path = os.path.join(E2E, f"audit-{mode}.log")
    return open(path).read().splitlines() if os.path.exists(path) else []


def mark(label):
    os.makedirs(E2E, exist_ok=True)
    row = f"{label}\t{time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}\tro={len(lines('ro'))}\trw={len(lines('rw'))}"
    with open(os.path.join(E2E, "ledger.tsv"), "a") as ledger:
        ledger.write(row + "\n")
    print(row)


def count(audit_lines, start, end=None):
    """Summarises the tool-call audit lines after line `start` (1-based count) up to `end`."""
    calls, codes, confirmations, by_tool, operations = 0, collections.Counter(), collections.Counter(), collections.Counter(), []
    for line in audit_lines[start:end]:
        try:
            entry = json.loads(line)
        except ValueError:
            continue
        if entry.get("msg") != "tool call":
            continue
        calls += 1
        by_tool[entry["tool"]] += 1
        operations.append(entry.get("operationId") or entry["tool"])
        if entry.get("code"):
            codes[entry["code"]] += 1
        if entry["tool"] == "execute_write":
            confirmations[entry.get("confirmation")] += 1
    return {
        "calls": calls,
        "errors": sum(codes.values()),
        "codes": dict(codes),
        "byTool": dict(by_tool),
        "executeWrite": by_tool.get("execute_write", 0),
        "confirmations": dict(confirmations),
        "operations": operations,
    }


def call(mode, tool, arguments):
    server = subprocess.Popen([os.path.join(ROOT, "scripts", "e2e-mcp-server.sh"), mode], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)

    def send(message):
        server.stdin.write(json.dumps(message) + "\n")
        server.stdin.flush()

    def receive(request_id):
        for line in server.stdout:
            message = json.loads(line)
            if message.get("id") == request_id:
                return message
        raise SystemExit("the server closed the connection without an answer")

    send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "e2e-audit", "version": "0"}}})
    receive(1)
    send({"jsonrpc": "2.0", "method": "notifications/initialized"})
    if tool == "tools/list":
        send({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
    else:
        send({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": tool, "arguments": arguments}})
    print(json.dumps(receive(2)))
    server.stdin.close()
    server.wait(timeout=10)


def self_test():
    sample = [
        '{"msg":"server started"}',
        '{"msg":"tool call","tool":"search_operations","operationId":"","code":""}',
        '{"msg":"tool call","tool":"execute_read","operationId":"trace_get","code":"langfuse_not_found"}',
        '{"msg":"tool call","tool":"execute_write","operationId":"trace_delete","code":"","confirmation":"requested"}',
        "not json",
        '{"msg":"tool call","tool":"execute_write","operationId":"trace_delete","code":"","confirmation":"accepted"}',
    ]
    whole = count(sample, 0)
    assert whole["calls"] == 4 and whole["errors"] == 1 and whole["codes"] == {"langfuse_not_found": 1}, whole
    assert whole["executeWrite"] == 2 and whole["confirmations"] == {"requested": 1, "accepted": 1}, whole
    assert whole["operations"] == ["search_operations", "trace_get", "trace_delete", "trace_delete"], whole
    assert count(sample, 2, 4)["calls"] == 2 and count(sample, 5)["calls"] == 1
    print("self-test ok")


def main(argv):
    if argv[1:] == ["--self-test"]:
        return self_test()
    if len(argv) == 3 and argv[1] == "mark":
        return mark(argv[2])
    if len(argv) in (4, 5) and argv[1] == "count" and argv[2] in ("ro", "rw"):
        result = count(lines(argv[2]), int(argv[3]), int(argv[4]) if len(argv) == 5 else None)
        operations = result.pop("operations")
        print(json.dumps(result))
        print("  " + " > ".join(operations))
        return None
    if len(argv) in (4, 5) and argv[1] == "call" and argv[2] in ("ro", "rw"):
        raw = argv[4] if len(argv) == 5 else "{}"
        return call(argv[2], argv[3], json.load(sys.stdin) if raw == "-" else json.loads(raw))
    raise SystemExit("usage: e2e-audit.py mark <label> | count <ro|rw> <from> [to] | call <ro|rw> <tool> [json|-] | --self-test")


if __name__ == "__main__":
    main(sys.argv)
