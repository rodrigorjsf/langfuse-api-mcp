#!/usr/bin/env python3
#
# WHAT: the small-model discovery eval (spec #68, ticket #75). It sends 11
#       natural-language intents to Haiku 4.5, one conversation each, with only
#       this server's tools (search_operations, describe_operation, execute_read,
#       get_trace_tree) and no word about the Langfuse API. An intent passes when
#       the model calls the expected tool with the expected operationId and key
#       parameters and the server accepts that call. It prints one PASS/FAIL
#       line per intent and the total.
# WHY:  M3 claims that a small model can go from an intent to the right
#       operation using the discovery tools alone. This run is the evidence. A
#       failing intent becomes a follow-up issue on M3 or M6 (a description,
#       hint or skill improvement), and the triage tools of ROADMAP "Later"
#       return only if the eval shows agents need them.
# WHEN: by hand, at the end of M3 and after any change to tool descriptions,
#       hints or the operation index; paste the output into the pull request.
#       It is not a CI gate: it costs API tokens and a model answer can vary.
# HOW:  ANTHROPIC_API_KEY=... python3 scripts/small-model-eval.py [--server BIN]
#           [--model ID] [--max-turns N] [--max-tokens N] [--timeout S]
#       --server     prebuilt langfuse-mcp binary (default: go build from this
#                    checkout into a temporary directory)
#       --model      default claude-haiku-4-5
#       --max-turns  model turns per intent (default 8)
#       --max-tokens output tokens per model turn (default 1024); a thinking
#                    model spends them on thinking first, so give it more
#       --timeout    seconds to wait for one model turn (default 120)
#       --only       comma-separated intent numbers to run (default: all), to
#                    rerun a failing intent once before calling it a failure
#
#       First recorded run (2026-09-27, #88): the local qwen3:8b instead of Haiku
#       4.5, by the maintainer's choice (no paid API), 7/11 passed; output in
#       docs/research/raw/2026-09-27-small-model-eval-qwen3-8b.md.
#       Against a local model: scripts/small-model-eval-local.sh starts the
#       Ollama stack of scripts/small-model-eval-ollama.yml, runs this script
#       with qwen3:8b through Ollama's Messages API, and stops the stack.
#
# The Anthropic key is read only from the ANTHROPIC_API_KEY environment
# variable, sent only in the x-api-key header, and never printed, logged or
# passed to the server process or to go build. ANTHROPIC_BASE_URL may point
# the run at another Messages API endpoint (https, or http on a loopback host
# only).
#
# No production data: the server runs against a fake Langfuse started here on
# 127.0.0.1. It answers as Langfuse 4.46.0 in events_only mode (health version,
# the v4 read and experiments sentinels on, the legacy family off, the profile
# of the pull-request integration run) and returns an empty page to every read.
# The eval judges which operation the model reaches, not the data it reads.
# The fake Langfuse keys below are placeholders; no real key is ever used.
#
# Standard library only, so the script needs nothing but python3 and, without
# --server, the Go toolchain.

import argparse
import ipaddress
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DEFAULT_MODEL = "claude-haiku-4-5"
ANTHROPIC_VERSION = "2023-06-01"
EXPECTED_TOOLS = {"search_operations", "describe_operation", "execute_read", "get_trace_tree"}
FAKE_VERSION = "4.46.0"
MCP_PROTOCOL_VERSION = "2025-06-18"

SYSTEM_PROMPT = (
    "You help a user investigate their LLM application's observability data "
    "through the tools you are given. Use the tools to answer the request. "
    "Do not ask the user questions; if a detail is missing, choose a sensible default."
)

# Each intent lists the calls that count as reaching it: a tool, the operationId
# (for execute_read) and the key parameters that call must carry. Every value is
# a literal written from the Langfuse API reference, never read from the catalog
# at run time, so the eval cannot agree with the server by construction. Where
# two operations answer an intent equally well on this deployment profile, both
# are listed.
INTENTS = [
    {
        "area": "traces",
        "intent": "Show me everything that happened inside trace 7f3c2a91-checkout, "
                  "as a tree of its observations.",
        "expect": [{"tool": "get_trace_tree", "arguments": {"traceId": "7f3c2a91-checkout"}}],
    },
    {
        "area": "observations",
        "intent": "List the generation observations named 'summarize-ticket'.",
        "expect": [{"tool": "execute_read", "operationId": "observations_getMany",
                    "parameters": {"name": "summarize-ticket", "type": "GENERATION"}}],
    },
    {
        "area": "observations",
        "intent": "Which observations belong to session sess-2024-17?",
        "expect": [{"tool": "execute_read", "operationId": "observations_getMany",
                    "parameters": {"sessionId": "sess-2024-17"}}],
    },
    {
        "area": "observations",
        "intent": "Find the observations with level ERROR for user u-5521.",
        "expect": [{"tool": "execute_read", "operationId": "observations_getMany",
                    "parameters": {"userId": "u-5521", "level": "ERROR"}}],
    },
    {
        "area": "scores",
        "intent": "List the scores named 'helpfulness' that were given to trace tr-88.",
        "expect": [{"tool": "execute_read", "operationId": "scoresV3_getManyV3",
                    "parameters": {"name": "helpfulness", "traceId": "tr-88"}}],
    },
    {
        "area": "scores",
        "intent": "Which score configs are defined in this project?",
        "expect": [{"tool": "execute_read", "operationId": "scoreConfigs_get", "parameters": {}}],
    },
    {
        "area": "prompts",
        "intent": "Fetch the prompt 'support-reply' with the label production.",
        "expect": [{"tool": "execute_read", "operationId": "prompts_get",
                    "parameters": {"promptName": "support-reply", "label": "production"}}],
    },
    {
        "area": "prompts",
        "intent": "List the prompts tagged 'billing'.",
        "expect": [{"tool": "execute_read", "operationId": "prompts_list",
                    "parameters": {"tag": "billing"}}],
    },
    {
        "area": "datasets",
        "intent": "Show me the dataset called golden-qa.",
        "expect": [{"tool": "execute_read", "operationId": "datasets_get",
                    "parameters": {"datasetName": "golden-qa"}}],
    },
    {
        "area": "datasets",
        "intent": "List the items of the dataset golden-qa.",
        "expect": [{"tool": "execute_read", "operationId": "datasetItems_list",
                    "parameters": {"datasetName": "golden-qa"}}],
    },
    {
        "area": "metrics",
        "intent": "What was the total cost per day over the last 7 days for traces named 'checkout'?",
        # Langfuse 4.x serves only Metrics v2: its query JSON has no traces view,
        # so the cost sits on the observations view, filtered by traceName.
        "expect": [{"tool": "execute_read", "operationId": "metrics_metrics",
                    "parameters": {}, "query": {"view": "observations", "measure": "totalCost",
                                                "filter": ["traceName", "checkout"],
                                                "granularity": "day"}}],
    },
]


class SetupError(Exception):
    """A failure before or around the model run: no key, no server, no API."""


# --- fake Langfuse -----------------------------------------------------------

class FakeLangfuse(BaseHTTPRequestHandler):
    """Langfuse 4.46.0 in events_only mode, with no data."""

    def do_GET(self):  # noqa: N802 (http.server naming)
        path = urllib.parse.urlparse(self.path).path
        if path == "/api/public/health":
            self.answer(200, {"status": "OK", "version": FAKE_VERSION})
        elif path == "/api/public/traces" or path.startswith("/api/public/traces/"):
            # The legacy family is off in events_only mode (ADR-0012 §3).
            self.answer(404, {"message": "This endpoint is not available in Langfuse v4 events_only mode."})
        else:
            self.answer(200, {"data": [], "meta": {}})

    def answer(self, status, body):
        raw = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *_):  # keep the eval output clean
        pass


def start_fake_langfuse():
    httpd = ThreadingHTTPServer(("127.0.0.1", 0), FakeLangfuse)
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    return httpd, f"http://127.0.0.1:{httpd.server_address[1]}"


# --- MCP client over stdio ----------------------------------------------------

class MCPSession:
    """The server process and a minimal MCP client session over its stdio
    (newline-delimited JSON-RPC)."""

    def __init__(self, binary, langfuse_url, workdir):
        # A clean environment: the Anthropic key and the operator's Langfuse
        # settings never reach the server; an empty config dir keeps the
        # operator's config file (ADR-0011) out of the run.
        home = os.path.join(workdir, "home")
        os.makedirs(home)
        env = {
            "HOME": home,
            "XDG_CONFIG_HOME": home,
            "LANGFUSE_BASE_URL": langfuse_url,
            "LANGFUSE_PUBLIC_KEY": "pk-lf-eval-placeholder",
            "LANGFUSE_SECRET_KEY": "sk-lf-eval-placeholder",
            "LANGFUSE_MCP_IGNORE_AMBIENT_CA": "true",
        }
        self.stderr = open(os.path.join(workdir, "server-stderr.log"), "w+b")
        self.proc = subprocess.Popen([binary], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                     stderr=self.stderr, env=env)
        self.next_id = 0
        self.request("initialize", {
            "protocolVersion": MCP_PROTOCOL_VERSION,
            "capabilities": {},
            "clientInfo": {"name": "small-model-eval", "version": "1"},
        })
        self.notify("notifications/initialized", {})

    def send(self, message):
        self.proc.stdin.write((json.dumps(message) + "\n").encode())
        self.proc.stdin.flush()

    def notify(self, method, params):
        self.send({"jsonrpc": "2.0", "method": method, "params": params})

    def request(self, method, params):
        self.next_id += 1
        self.send({"jsonrpc": "2.0", "id": self.next_id, "method": method, "params": params})
        while True:
            line = self.proc.stdout.readline()
            if not line:
                raise SetupError(f"the server exited during {method}:\n{self.server_log()}")
            message = json.loads(line)
            if message.get("id") != self.next_id:
                continue  # a notification or a server request: not ours
            if "error" in message:
                raise SetupError(f"{method} failed: {message['error'].get('message', '')}")
            return message["result"]

    def server_log(self):
        self.stderr.seek(0)
        return self.stderr.read().decode(errors="replace")

    def close(self):
        self.proc.stdin.close()
        try:
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()
        self.stderr.close()


# --- Anthropic Messages API ---------------------------------------------------

def anthropic_endpoint():
    base = os.environ.get("ANTHROPIC_BASE_URL", "https://api.anthropic.com").rstrip("/")
    url = urllib.parse.urlparse(base)
    if url.scheme != "https" and not (url.scheme == "http" and is_loopback(url.hostname)):
        raise SetupError("ANTHROPIC_BASE_URL must be https, or http on a loopback host")
    return base + "/v1/messages"


def is_loopback(host):
    if host == "localhost":
        return True
    try:
        return ipaddress.ip_address(host or "").is_loopback
    except ValueError:
        return False


def create_message(endpoint, key, body, timeout):
    request = urllib.request.Request(endpoint, data=json.dumps(body).encode(), method="POST", headers={
        "x-api-key": key,
        "anthropic-version": ANTHROPIC_VERSION,
        "content-type": "application/json",
    })
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return json.load(response)
    except urllib.error.HTTPError as err:
        # Only the status and the API's error type: never the request.
        try:
            kind = json.load(err).get("error", {}).get("type", "unknown")
        except ValueError:
            kind = "unknown"
        raise SetupError(f"Messages API answered {err.code} ({kind})") from None
    except urllib.error.URLError as err:
        raise SetupError(f"Messages API unreachable: {err.reason}") from None
    except TimeoutError:
        raise SetupError(f"Messages API did not answer within {timeout} s") from None


# --- the eval -----------------------------------------------------------------

def matches(call, expected):
    """Whether one tool call reaches one expected call."""
    if call["tool"] != expected["tool"]:
        return False
    args = call["arguments"] if isinstance(call["arguments"], dict) else {}
    if expected["tool"] == "execute_read":
        if args.get("operationId") != expected["operationId"]:
            return False
        got = args.get("parameters") if isinstance(args.get("parameters"), dict) else {}
        want = expected["parameters"]
        if "query" in expected and not metrics_query_matches(got.get("query"), expected["query"]):
            return False
    else:
        got, want = args, expected["arguments"]
    return all(same_value(got.get(k), v) for k, v in want.items())


def metrics_query_matches(raw, want):
    """Whether a metrics query JSON has the view, a measure and a filter wanted."""
    try:
        query = json.loads(raw) if isinstance(raw, str) else raw
    except ValueError:
        return False
    if not isinstance(query, dict) or query.get("view") != want["view"]:
        return False
    measures = [m.get("measure") for m in query.get("metrics") or [] if isinstance(m, dict)]
    column, value = want["filter"]
    filters = [f for f in query.get("filters") or [] if isinstance(f, dict) and f.get("column") == column]
    time = query.get("timeDimension") if isinstance(query.get("timeDimension"), dict) else {}
    return (want["measure"] in measures
            and any(same_value(f.get("value"), value) for f in filters)
            and time.get("granularity") == want["granularity"])


def same_value(got, want):
    if isinstance(got, list):
        return any(same_value(g, want) for g in got)
    return got is not None and str(got).casefold() == str(want).casefold()


def describe(call):
    args = call["arguments"] if isinstance(call["arguments"], dict) else {}
    if call["tool"] == "execute_read":
        return f"execute_read {args.get('operationId')} {json.dumps(args.get('parameters', {}), sort_keys=True)}"
    return f"{call['tool']} {json.dumps(args, sort_keys=True)}"


def describe_expected(expected):
    if expected["tool"] != "execute_read":
        return describe(expected)
    line = describe({"tool": "execute_read", "arguments": {
        "operationId": expected["operationId"], "parameters": expected["parameters"]}})
    if "query" in expected:
        line += f" with query {json.dumps(expected['query'])}"
    return line


def run_intent(server, tools, endpoint, key, args, item):
    """Runs one conversation; returns (passed, the calls the model made, how it ended)."""
    messages = [{"role": "user", "content": item["intent"]}]
    calls = []
    ended = f"max turns ({args.max_turns})"
    for _ in range(args.max_turns):
        reply = create_message(endpoint, key, {
            "model": args.model,
            "max_tokens": args.max_tokens,
            "temperature": 0,
            "system": SYSTEM_PROMPT,
            "tools": tools,
            "messages": messages,
        }, args.timeout)
        messages.append({"role": "assistant", "content": reply["content"]})
        uses = [block for block in reply["content"] if block.get("type") == "tool_use"]
        if not uses:
            ended = f"no tool call; stop_reason {reply.get('stop_reason')}"
            break
        results = []
        for use in uses:
            call = {"tool": use["name"], "arguments": use.get("input", {})}
            calls.append(call)
            result = server.request("tools/call", {"name": use["name"], "arguments": use.get("input", {})})
            # A pass is the expected call that the server also accepts.
            if not result.get("isError") and any(matches(call, e) for e in item["expect"]):
                return True, calls, "expected call"
            text = "\n".join(c.get("text", "") for c in result.get("content", []) if c.get("type") == "text")
            results.append({"type": "tool_result", "tool_use_id": use["id"],
                            "content": text, "is_error": bool(result.get("isError"))})
        messages.append({"role": "user", "content": results})
    return False, calls, ended


def build_server(workdir):
    if shutil.which("go") is None:
        raise SetupError("go is not on PATH; pass --server with a prebuilt binary")
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    binary = os.path.join(workdir, "bin", "langfuse-mcp")
    # The Anthropic key stays out of the toolchain's environment too.
    env = {k: v for k, v in os.environ.items() if not k.startswith("ANTHROPIC_")}
    subprocess.run(["go", "build", "-o", binary, "./cmd/langfuse-mcp"], cwd=root, env=env, check=True)
    return binary


def main():
    parser = argparse.ArgumentParser(description="Small-model discovery eval (spec #68).")
    parser.add_argument("--server", help="prebuilt langfuse-mcp binary")
    parser.add_argument("--model", default=DEFAULT_MODEL)
    parser.add_argument("--max-turns", type=int, default=8)
    parser.add_argument("--max-tokens", type=int, default=1024)
    parser.add_argument("--timeout", type=float, default=120)
    parser.add_argument("--only", help="comma-separated intent numbers to run (default: all)")
    args = parser.parse_args()
    selected = list(range(1, len(INTENTS) + 1))
    if args.only:
        try:
            selected = sorted({int(n) for n in args.only.split(",")})
        except ValueError:
            parser.error("--only takes comma-separated intent numbers")
        if not all(1 <= n <= len(INTENTS) for n in selected):
            parser.error(f"--only takes intent numbers from 1 to {len(INTENTS)}")

    key = os.environ.get("ANTHROPIC_API_KEY", "")
    if not key:
        print("ANTHROPIC_API_KEY is not set", file=sys.stderr)
        return 2

    with tempfile.TemporaryDirectory(prefix="small-model-eval-") as workdir:
        httpd, langfuse_url = start_fake_langfuse()
        server = None
        try:
            endpoint = anthropic_endpoint()
            binary = args.server or build_server(workdir)
            server = MCPSession(binary, langfuse_url, workdir)
            listed = server.request("tools/list", {})["tools"]
            names = {tool["name"] for tool in listed}
            if names != EXPECTED_TOOLS:
                raise SetupError(f"the server offers {sorted(names)}, want {sorted(EXPECTED_TOOLS)}")
            tools = [{"name": t["name"], "description": t.get("description", ""),
                      "input_schema": t["inputSchema"]} for t in listed]

            print(f"model {args.model}; max tokens {args.max_tokens}; fake Langfuse {FAKE_VERSION} events_only; "
                  f"tools {', '.join(sorted(names))}")
            passed = 0
            for number in selected:
                item = INTENTS[number - 1]
                ok, calls, ended = run_intent(server, tools, endpoint, key, args, item)
                passed += ok
                print(f"{'PASS' if ok else 'FAIL'} {number:02d} [{item['area']}] {item['intent']}")
                for call in calls:
                    print(f"       {describe(call)}")
                if not ok:
                    print(f"       ended: {ended}")
                    print(f"       want: {' | '.join(describe_expected(e) for e in item['expect'])}")
            print(f"TOTAL {passed}/{len(selected)} passed")
            return 0 if passed == len(selected) else 1
        except SetupError as err:
            print(f"eval aborted: {err}", file=sys.stderr)
            return 2
        finally:
            if server is not None:
                server.close()
            httpd.shutdown()


if __name__ == "__main__":
    sys.exit(main())
