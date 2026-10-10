package flightrec

import (
	"encoding/json"
	"strings"
	"testing"
)

// corpusMarker is the secret marker every positive corpus entry carries; no
// sanitized output may contain it.
const corpusMarker = "LEAKVAL9"

// positiveStrings are secret-bearing free-text strings (log lines, errors,
// headers, command lines, URLs). Each is sanitized bare, as a map value and as
// an array element.
var positiveStrings = []string{
	// key naming variants: prefix, suffix, case, separators
	"db_password: LEAKVAL9", "DB_PASSWORD: LEAKVAL9", "dbPassword: LEAKVAL9", "db.password=LEAKVAL9", "db-password = LEAKVAL9",
	"GITHUB_TOKEN: LEAKVAL9", "github_token=LEAKVAL9", "refresh_token: LEAKVAL9", "id_token=LEAKVAL9", "access-token: LEAKVAL9",
	"aws_secret_access_key = LEAKVAL9", "client_secret=LEAKVAL9", "CLIENT_SECRET: LEAKVAL9", "secret_key: LEAKVAL9",
	"private_key: LEAKVAL9", "privateKey=LEAKVAL9", "ssh_key: LEAKVAL9", "access_key_id=LEAKVAL9", "api_key=LEAKVAL9",
	"API-KEY: LEAKVAL9", "apikey: LEAKVAL9", "x-api-key: LEAKVAL9", "passphrase: LEAKVAL9", "passphrase=LEAKVAL9",
	"passwd=LEAKVAL9", "pwd: LEAKVAL9", "pass=LEAKVAL9", "db_pass=LEAKVAL9", "pw: LEAKVAL9", "otp=LEAKVAL9", "seed: LEAKVAL9",
	"mnemonic: LEAKVAL9", "hmac_secret=LEAKVAL9", "signing_key: LEAKVAL9", "encryption-key=LEAKVAL9", "connection_string=LEAKVAL9",
	"session_id: LEAKVAL9", "sessionid=LEAKVAL9", "jwt: LEAKVAL9", "auth=LEAKVAL9", "x-auth: LEAKVAL9", "credentials: LEAKVAL9",
	"my.service.credential=LEAKVAL9", "bearer: LEAKVAL9",
	// spacing and quoting
	"password:LEAKVAL9", "password :LEAKVAL9", "password : LEAKVAL9", `password="LEAKVAL9"`, `password: 'LEAKVAL9'`,
	`password: "two LEAKVAL9 words"`, `secret='has LEAKVAL9 spaces'`, "password LEAKVAL9", "Passwd LEAKVAL9", "token LEAKVAL9",
	`"password": "LEAKVAL9"`, `'token': 'LEAKVAL9'`,
	// JSON / YAML / shell inside strings
	`{"password":"LEAKVAL9"}`, `{"user":"a","db_password":"LEAKVAL9","n":1}`, `{"api_key": "LEAKVAL9"}`,
	`{"client_secret":"two LEAKVAL9 words"}`, `{"token":"LEAKVAL9"}`, `config: {secret: LEAKVAL9}`,
	"api_key: LEAKVAL9", "  password: LEAKVAL9", "- secret: LEAKVAL9", "export API_TOKEN=LEAKVAL9", "export DB_PASSWORD=LEAKVAL9 && run",
	"FOO=1 TOKEN=LEAKVAL9 ./run", "API_KEY=LEAKVAL9 node app.js", `API_KEY="LEAKVAL9 with space" cmd`, "PGPASSWORD=LEAKVAL9 psql",
	"db_pass=LEAKVAL9 psql", "token=LEAKVAL9 make test", "env AWS_SECRET_ACCESS_KEY=LEAKVAL9 aws s3 ls",
	// HTTP headers
	"Authorization: Bearer LEAKVAL9", "authorization: Basic LEAKVAL9", "Authorization: Token LEAKVAL9", "Authorization: Digest username=a, response=LEAKVAL9",
	"Authorization: AWS4-HMAC-SHA256 Credential=LEAKVAL9/x, Signature=abcd", "Proxy-Authorization: Basic LEAKVAL9", "Authorization: LEAKVAL9",
	"Cookie: a=b; sid=LEAKVAL9", "cookie: session=LEAKVAL9", "Set-Cookie: sid=LEAKVAL9; Path=/; HttpOnly", "Cookie: LEAKVAL9",
	"X-Auth-Token: LEAKVAL9", "X-Amz-Security-Token: LEAKVAL9", "X-Api-Key: LEAKVAL9", "X-GitHub-Token: LEAKVAL9",
	`request headers {"Authorization":"Bearer LEAKVAL9"}`, "request failed: Bearer LEAKVAL9", "got 401 with Basic LEAKVAL9 credentials",
	// CLI flags
	"--password LEAKVAL9", "--password=LEAKVAL9", "--token=LEAKVAL9", "--api-key LEAKVAL9", "run --api-key LEAKVAL9 now",
	"--secret-key=LEAKVAL9", "--private-key LEAKVAL9", "--client_secret LEAKVAL9", "--client-secret=LEAKVAL9", "--access-key LEAKVAL9",
	"--access_token=LEAKVAL9", "--db_password LEAKVAL9", "--passphrase LEAKVAL9", "--pass LEAKVAL9", "--bearer LEAKVAL9",
	"--auth LEAKVAL9", "--cookie LEAKVAL9", "--api_key=LEAKVAL9", "-token LEAKVAL9", "-password=LEAKVAL9",
	"deploy --env prod --secret LEAKVAL9 --verbose", "tool --pass=LEAKVAL9 --other 1", "login --user a --passphrase LEAKVAL9",
	// URLs
	"https://LEAKVAL9@h/", "https://user:LEAKVAL9@host/path", "postgres://app:LEAKVAL9@db:5432/x", "ssh://LEAKVAL9@h/x",
	"git clone https://ghp_LEAKVAL9abc@github.com/a/b", "GET /v1/x?access_token=LEAKVAL9", "GET /v1/x?token=LEAKVAL9&b=1",
	"https://h/p?a=1&apikey=LEAKVAL9", "https://h/p?api_key=LEAKVAL9&x=2", "https://h/cb?code=1&client_secret=LEAKVAL9",
	"https://h/p?password=LEAKVAL9", "https://h/p?X-Amz-Signature=LEAKVAL9", "wss://h/s?auth=LEAKVAL9&v=1",
	// well-known token prefixes
	"ghp_LEAKVAL9abc", "gho_LEAKVAL9abc", "ghs_LEAKVAL9abc", "github_pat_LEAKVAL9abc", "glpat-LEAKVAL9abc", "xoxb-LEAKVAL9abc",
	"xoxp-LEAKVAL9abc", "xapp-LEAKVAL9abc", "sk-LEAKVAL9abc", "sk_live_LEAKVAL9abc", "sk-ant-LEAKVAL9abc", "npm_LEAKVAL9abc",
	"AIzaLEAKVAL9abcdefgh", "AKIALEAKVAL9ABCDEFGH", "ASIALEAKVAL9ABCDEFGH", "hf_LEAKVAL9abcdefgh", "pypi-LEAKVAL9abcdefgh",
	"ya29.LEAKVAL9abcdefgh", "SG.LEAKVAL9abcdefgh", "pat_LEAKVAL9abc", "using ghp_LEAKVAL9abc in CI", "cloning with glpat-LEAKVAL9abc and more",
	// JWT, PEM, ssh key blocks
	"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJMRUFLVkFMOSJ9.LEAKVAL9sig", "jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.LEAKVAL9sig end",
	"-----BEGIN RSA PRIVATE KEY-----LEAKVAL9", "-----BEGIN PRIVATE KEY----- LEAKVAL9", "-----BEGIN OPENSSH PRIVATE KEY----- LEAKVAL9",
	"-----BEGIN PGP PRIVATE KEY BLOCK-----LEAKVAL9", "-----BEGIN EC PRIVATE KEY-----\\nLEAKVAL9\\n-----END EC PRIVATE KEY-----",
	"key ssh-rsa AAAAB3NzaC1yc2ELEAKVAL9 user@host", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5LEAKVAL9",
	// wrapped errors
	"open store: dial: password authentication failed password=LEAKVAL9", "exec: \"x\": failed: token=LEAKVAL9: exit status 1",
	"provider: 401: Authorization: Bearer LEAKVAL9: retry later", "bootstrap: resolve: api_key: LEAKVAL9: giving up",
	"git push https://user:LEAKVAL9@h/r.git: denied", "build failed: GITHUB_TOKEN=LEAKVAL9 expired: x: y",
	// unicode and invisible evasions
	"pass\u200bword: LEAKVAL9", "tok\u200ben=LEAKVAL9", "api\u2060_key: LEAKVAL9", "pass\u00adword=LEAKVAL9", "Bearer\u00a0LEAKVAL9",
	"password:\u00a0LEAKVAL9", "password\u2003LEAKVAL9", "token\u3000LEAKVAL9", "\uff50\uff41\uff53\uff53\uff57\uff4f\uff52\uff44\uff1a LEAKVAL9",
	"\uff54\uff4f\uff4b\uff45\uff4e\uff1dLEAKVAL9", "Cookie\u00a0: a=LEAKVAL9", "Author\u200bization: Bearer LEAKVAL9",
	"secret\u2028LEAKVAL9", "api\ufeff_key=LEAKVAL9",
}

// positivePayloads are structured payloads that must not retain the marker.
var positivePayloads = []any{
	map[string]any{"password": corpusMarker}, map[string]any{"PASSWORD": corpusMarker}, map[string]any{"db_password": corpusMarker},
	map[string]any{"dbPassword": corpusMarker}, map[string]any{"db.password": corpusMarker}, map[string]any{"db-password": corpusMarker},
	map[string]any{"token": corpusMarker}, map[string]any{"GITHUB_TOKEN": corpusMarker}, map[string]any{"refresh_token": corpusMarker},
	map[string]any{"access_token": corpusMarker}, map[string]any{"id_token": corpusMarker}, map[string]any{"X-Auth-Token": corpusMarker},
	map[string]any{"client_secret": corpusMarker}, map[string]any{"aws_secret_access_key": corpusMarker}, map[string]any{"secretKey": corpusMarker},
	map[string]any{"private_key": corpusMarker}, map[string]any{"privateKey": corpusMarker}, map[string]any{"access_key": corpusMarker},
	map[string]any{"accessKey": corpusMarker}, map[string]any{"api_key": corpusMarker}, map[string]any{"apiKey": corpusMarker},
	map[string]any{"x-api-key": corpusMarker}, map[string]any{"passphrase": corpusMarker}, map[string]any{"pass": corpusMarker},
	map[string]any{"pw": corpusMarker}, map[string]any{"otp": corpusMarker}, map[string]any{"seed": corpusMarker},
	map[string]any{"mnemonic": corpusMarker}, map[string]any{"hmac": corpusMarker}, map[string]any{"hmac_key": corpusMarker},
	map[string]any{"signing_key": corpusMarker}, map[string]any{"encryption_key": corpusMarker}, map[string]any{"dsn": corpusMarker},
	map[string]any{"connection_string": corpusMarker}, map[string]any{"authorization": corpusMarker}, map[string]any{"Authorization": "Bearer " + corpusMarker},
	map[string]any{"cookie": corpusMarker}, map[string]any{"Set-Cookie": corpusMarker}, map[string]any{"session_id": corpusMarker},
	map[string]any{"jwt": corpusMarker}, map[string]any{"credentials": corpusMarker}, map[string]any{"ssh_key": corpusMarker},
	map[string]any{"signature": corpusMarker}, map[string]any{"bearer": corpusMarker}, map[string]any{"auth": corpusMarker},
	map[string]any{"pa\u200bssword": corpusMarker}, map[string]any{"p\u0430ssword": corpusMarker}, map[string]any{"\uff30\uff21\uff33\uff33": corpusMarker},
	map[string]any{"input_tokens_secret": corpusMarker}, map[string]any{"access_tokens": corpusMarker}, map[string]any{"tokens": corpusMarker},
	map[string]any{"input_tokens": corpusMarker}, map[string]any{"total_tokens": []any{corpusMarker}},
	map[string]any{"env": map[string]any{"HOME": corpusMarker}}, map[string]any{"environ": []any{"PATH=" + corpusMarker}},
	map[string]any{"config": map[string]any{"db": map[string]any{"password": corpusMarker}}},
	map[string]any{"a": []any{map[string]any{"token": corpusMarker}}},
	map[string]any{"headers": map[string]any{"Authorization": "Basic " + corpusMarker, "Accept": "x"}},
	map[string]any{"raw": map[string]any{"stdout": corpusMarker}, "stderr": corpusMarker}, map[string]any{"prompt": corpusMarker},
	map[string]any{"contents": corpusMarker}, map[string]any{"configcontents": corpusMarker},
	[]any{"--password", corpusMarker}, []any{"--client_secret", corpusMarker}, []any{"--secret-key", corpusMarker},
	[]any{"--private-key", corpusMarker}, []any{"--access-key", corpusMarker}, []any{"--access_token", corpusMarker},
	[]any{"--db_password", corpusMarker}, []any{"--passphrase", corpusMarker}, []any{"--pass", corpusMarker}, []any{"--bearer", corpusMarker},
	[]any{"--auth", corpusMarker}, []any{"--cookie", corpusMarker}, []any{"--api_key", corpusMarker}, []any{"--api-key", corpusMarker},
	[]any{"-token", corpusMarker}, []any{"--TOKEN", corpusMarker}, []any{"--Client-Secret", corpusMarker},
	[]any{"run", "--env", "prod", "--secret", corpusMarker, "--verbose"}, []any{"--token=" + corpusMarker}, []any{"--secret-key=" + corpusMarker, "next"},
	[]any{"curl", "-H", "Authorization: Bearer " + corpusMarker, "https://h/"}, []any{"curl", "-H", "Cookie: sid=" + corpusMarker},
	[]any{"psql", "postgres://u:" + corpusMarker + "@h/db"}, []any{"x", []any{"--password", corpusMarker}},
	[]any{[]byte(corpusMarker)}, map[string]any{"blob": []byte(corpusMarker)}, []byte(corpusMarker),
	map[string]any{"err": "dial failed: token=" + corpusMarker}, map[string]any{"cmd": []any{"git", "-c", "http.extraHeader=Authorization: Bearer " + corpusMarker}},
	map[string]any{"name": "ghp_" + corpusMarker + "abc"}, map[string]any{"list": []any{"ok", "AKIA" + corpusMarker + "ABCDEFGH"}},
}

// negativeStrings are benign diagnostics that must survive unchanged.
var negativeStrings = []string{
	"QUOTA_EXCEEDED", "reason: timeout", "reason_code=E_TIMEOUT", "task_01HZX:attempt-3", "run_01HZX9ABCDEFGHJKMNPQRSTVWX", "nod_01HZX9ABCDEFGHJKMNPQRSTVWX",
	"gpt-4", "gpt-4o-mini", "claude-sonnet-5", "claude-opus-4", "gemini-2.5-pro", "main.go:42", "internal/flightrec/sanitize.go",
	"internal/flightrec/sanitize_corpus_test.go:100", "README.md", "docs/OBSERVABILITY.md", "duration 1.5s", "elapsed=250ms", "retry in 30 seconds",
	"timeout after 2m0s", "attempt 3 of 5", "count: 12", "exit status 1", "exit code 137", "lines: 120 added, 4 removed",
	"token-expired", "tokens exhausted", "tokenizer ready", "disk-usage-high", "task-abcdefghi done", "risk-assessment complete",
	"password", "the secret", "basic ok", "bearer", "pallbearer of bad news", "secretary", "tokens", "authorize", "authored by team",
	"git@github.com:a/b.git", "https://example.com/path?x=1", "https://example.com/a/b?page=2&sort=asc", "http://localhost:8080/health",
	"state: running", "status=ok", "phase: validate", "level=info msg=started", "mode: journal", "source: env_trace_dir",
	"selected_source=user_home", "error_code=mkdir_failed", "sha256:" + strings.Repeat("ab", 32), "v1.2.3", "go1.26.0", "linux/amd64",
	"cache hit ratio 0.93", "queue depth 7", "worker 3 idle", "branch feat/flightrec-wu-b", "commit 9f4d112", "bypass enabled", "compass north",
	"passed", "passenger", "seeded", "seedling", "otpional", "author: alice", "authority granted", "keyboard layout us", "monkey patch applied",
	"max 4096 reached", "context window 200000", "temperature 0.2", "model gpt-4o", "retrying", "unauthorized access blocked",
}

// negativePayloads are benign structured payloads that must survive.
var negativePayloads = []any{
	map[string]any{"reason": "timeout", "attempt": 3, "duration_ms": 120},
	map[string]any{"input_tokens": 10, "output_tokens": 20, "total_tokens": 30, "max_tokens": 4096, "cached_tokens": 5},
	map[string]any{"model": "gpt-4o-mini", "path": "internal/x.go:1"},
	[]any{"deploy", "--env", "prod", "--verbose", "--count", "3"},
	[]any{"go", "test", "-race", "-count=1", "./..."},
}

func TestSanitizerCorpusSizes(t *testing.T) {
	if p, n := len(positiveStrings)*3+len(positivePayloads), len(negativeStrings); p < 150 || len(positiveStrings) < 150 || n < 60 {
		t.Fatalf("corpus too small: positives %d (strings %d), negatives %d", p, len(positiveStrings), n)
	}
}

// containsMarker reports whether the marker (or a case variant) survived.
func containsMarker(out string) bool {
	return strings.Contains(strings.ToLower(out), strings.ToLower(corpusMarker))
}

func TestSanitizerCorpusPositiveStrings(t *testing.T) {
	s := DefaultSanitizer()
	for _, in := range positiveStrings {
		for _, v := range []any{in, map[string]any{"note": in}, []any{"a", in}} {
			got, san := sanitized(t, s, v)
			if containsMarker(got) || san.Redactions == 0 {
				t.Errorf("%q leaked or was not counted: %s %+v", in, got, san)
			}
		}
	}
}

func TestSanitizerCorpusPositivePayloads(t *testing.T) {
	s := DefaultSanitizer()
	for i, v := range positivePayloads {
		got, _ := sanitized(t, s, v)
		if containsMarker(got) {
			b, _ := json.Marshal(v)
			t.Errorf("payload %d leaked: in %s out %s", i, b, got)
		}
	}
}

func TestSanitizerCorpusNegative(t *testing.T) {
	s := DefaultSanitizer()
	for _, in := range negativeStrings {
		want, _ := json.Marshal(in)
		if got, san := sanitized(t, s, in); got != string(want) || san.Redactions != 0 {
			t.Errorf("%q over-redacted: %s %+v", in, got, san)
		}
	}
	for i, v := range negativePayloads {
		want, _ := json.Marshal(v)
		if got, san := sanitized(t, s, v); got != string(want) || san.Redactions != 0 {
			t.Errorf("payload %d over-redacted: %s", i, got)
		}
	}
}

// TestSanitizerScalarCorpus: scalar fields reject or redact secrets too.
func TestSanitizerScalarCorpus(t *testing.T) {
	s := DefaultSanitizer()
	for _, in := range []string{"db_password=LEAKVAL9", "GITHUB_TOKEN=LEAKVAL9", "ghp_LEAKVAL9abc", "https://h/p?access_token=LEAKVAL9", "api_key:LEAKVAL9"} {
		if got := s.Scalar(ScalarLocator, in); containsMarker(got) {
			t.Errorf("scalar %q leaked: %s", in, got)
		}
	}
}
