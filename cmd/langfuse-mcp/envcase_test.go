package main

import (
	"encoding/base64"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Issue #62 (spec #150), seam: the executable. On Windows, variable names are
// case-insensitive, so a setting stored with any casing is honoured, as by
// every other Windows program; on Linux and macOS names stay case-sensitive,
// so no casing shadows another. On Windows, os/exec keeps only the last entry
// of names that differ in case, so the child sees exactly the spelling a test
// sets last.

func skipUnlessWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("variable names are case-insensitive on Windows only")
	}
}

func TestOnWindowsExecutableReachesLangfuseThroughAProxyVariableOfAnyCasing(t *testing.T) {
	skipUnlessWindows(t)
	t.Parallel()
	const user, password = "proxyuser-4b2d", "proxysecret-e81a" //nolint:gosec // G101: fake proxy credentials
	ca, caFile := newCertAuthority(t, "proxy test CA")
	proxy := newConnectProxy(t, tunnelLangfuse(t, ca).Listener.Addr().String())
	s := startStdio(t, "LANGFUSE_BASE_URL=https://"+tunnelHost, "LANGFUSE_CA_CERT="+caFile,
		"Https_Proxy=http://"+user+":"+password+"@"+proxy.url.Host)

	throughTheProxy(t, s, proxy)

	_, auths := proxy.seen()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	if slices.ContainsFunc(auths, func(a string) bool { return a != want }) {
		t.Errorf("the proxy received Proxy-Authorization %q, want %q on every CONNECT", auths, want)
	}
	got := proxyLogLine(t, s.stderr.Bytes())
	if got["endpoint"] != "http://"+proxy.url.Host || got["variable"] != "HTTPS_PROXY" || got["source"] != "environment" {
		t.Errorf("proxy log line = %v, want endpoint http://%s, variable HTTPS_PROXY, source environment", got, proxy.url.Host)
	}
	for _, secret := range []string{user, password} {
		if strings.Contains(s.stderr.String(), secret) {
			t.Errorf("stderr contains the proxy credential %q:\n%s", secret, s.stderr)
		}
	}
}

func TestOnWindowsStartupFailsNamingAnInvalidProxyVariableOfAnyCasingButNeverItsValue(t *testing.T) {
	skipUnlessWindows(t)
	t.Parallel()
	const value = "htps://proxyuser:proxysecret-5d0c@proxy.example.com:8080" //nolint:gosec // G101: a fake proxy credential

	stderr, err := runExecutable(t, "Https_Proxy="+value)

	if err == nil {
		t.Fatalf("executable exited 0 with an invalid Https_Proxy; stderr:\n%s", stderr)
	}
	if msg := startupError(t, stderr); !strings.Contains(msg, "HTTPS_PROXY") || !strings.Contains(msg, "environment") {
		t.Errorf("startup error %q does not name HTTPS_PROXY and its source", msg)
	}
	for _, leak := range []string{value, "proxysecret-5d0c", "proxy.example.com"} {
		if strings.Contains(string(stderr), leak) {
			t.Errorf("stderr contains %q from the proxy value:\n%s", leak, stderr)
		}
	}
}

func TestOnWindowsExecutableFindsALangfuseSettingOfAnyCasing(t *testing.T) {
	skipUnlessWindows(t)
	t.Parallel()

	stderr, err := runExecutable(t, "LANGFUSE_BASE_URL=https://cloud.langfuse.com", "LANGFUSE_MCP_RATE_LIMIT=",
		"Langfuse_Mcp_Rate_Limit=17", noLangfuseProxy)
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	for _, line := range logLines(t, stderr) {
		if line["msg"] == "rate limit" {
			if line["perMinute"] != float64(17) || line["source"] != "explicit" {
				t.Errorf("rate limit line = %v, want perMinute 17, source explicit", line)
			}
			return
		}
	}
	t.Fatalf("no \"rate limit\" line in the startup log:\n%s", stderr)
}

func TestOutsideWindowsExecutableIgnoresAProxyVariableOfOtherCasing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("variable names are case-sensitive outside Windows only")
	}
	t.Parallel()

	stderr, err := runExecutable(t, "Https_Proxy=http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	if got := proxyLogLine(t, stderr); got["endpoint"] != "none" {
		t.Errorf("proxy log line = %v, want endpoint none: Https_Proxy is not HTTPS_PROXY here", got)
	}
}
