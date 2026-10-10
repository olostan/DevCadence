package flightrec

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Round-3 corpus. Every "@@" below stands for the secret marker ZQLEAKZ9; no
// sanitized output may contain it (case-insensitively).
const round3Marker = "ZQLEAKZ9"

func expandMarker(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ReplaceAll(s, "@@", round3Marker)
	}
	return out
}

// round3Positives are secret-bearing strings, each tested bare, as a map value
// and as an array element.
var round3Positives = expandMarker([]string{
	// reviewer repros: double-encoded JSON
	`{\"password\":\"@@\"}`, `body: "{\"api_key\":\"@@\"}"`,
	// reviewer repros: signatures
	`https://x.blob.core.windows.net/c/b?sv=2020&se=2030&sig=@@%3D`, `GET /x?sig=@@`, `signature=@@`, `Signature: @@`,
	`X-Goog-Signature=@@`, `AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20200101/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=@@`,
	// reviewer repros: key prefixes
	`rk_live_@@`, `rk_test_@@`, `sk_live_@@`, `sk_test_@@`, `whsec_@@`, `shpat_@@`, `shpca_@@`, `shppa_@@`, `dop_v1_@@`,
	`xoxe-@@`, `xoxa-@@`, `xoxr-@@`, `SG.@@`, `pypi-@@`, "SK" + strings.Repeat("0123456789abcdef", 2), /* built at runtime: a literal trips push protection */
	`https://hooks.slack.com/services/T0000/B0000/@@`, `https://discord.com/api/webhooks/123456789/@@`,
	`https://discordapp.com/api/webhooks/123456789/@@-x`,
	// reviewer repros: _pass / -pw suffixes
	`db_pass=@@`, `DB_PASS=@@`, `admin_pass: @@`, `mysql_pw=@@`, `--db-pass @@`, `--mysql-pw @@`, `--db-pass=@@`,
	// reviewer repros: other operators
	`password => @@`, `'password' => '@@'`, `TOKEN := @@`, `TOKEN ?= @@`, `password:= @@`, `password is @@`, `password was @@`,
	`curl -u admin:@@ https://h/x`, `curl --user admin:@@ https://h/x`, `curl --proxy-user p:@@ https://h/x`, `--user=a:@@`,
	// double-encoded and nested JSON
	`{\"token\":\"@@\"}`, `{\"client_secret\": \"@@\"}`, `{\\\"password\\\":\\\"@@\\\"}`, `{\"a\":{\"password\":\"@@\"}}`,
	`"{\"user\":\"a\",\"secret\":\"@@\"}"`, `payload=\"{\\\"api_key\\\":\\\"@@\\\"}\"`, `{\'password\':\'@@\'}`,
	`log: body="{\"authorization\":\"Bearer @@\"}"`, `{\"private_key\":\"@@\"}`, `{\"nested\":\"{\\\"token\\\":\\\"@@\\\"}\"}`,
	`{\"Cookie\":\"sid=@@\"}`, `{\"db\":{\"pass\":\"@@\"}}`, `{\"signature\":\"@@\"}`, `{\"sig\":\"@@\"}`,
	`https:\/\/u:@@@h\/x`, `{\"url\":\"https:\/\/h\/p?access_token=@@\"}`, `{\"x\":\"a\",\"passphrase\":\"@@\"}`,
	// query-string and fragment credentials
	`https://h/cb#access_token=@@&state=1`, `https://h/cb?access_token=@@`, `https://h/p?a=1&sig=@@`, `https://h/p?sig=@@&a=1`,
	`https://h/p?key=@@`, `https://h/p?a=1&key=@@&b=2`, `https://h/p;key=@@`, `GET /v1?apikey=@@ HTTP/1.1`, `GET /v1?secret=@@`,
	`GET /v1?password=@@&u=a`, `GET /v1?token=@@`, `https://h/p?X-Goog-Signature=@@`, `https://h/p?signature=@@`,
	`https://h/p?refresh_token=@@`, `https://h/p?client_secret=@@`, `redirect to https://h/x?id_token=@@#frag`,
	`https://s3.amazonaws.com/b/k?X-Amz-Signature=@@&X-Amz-Expires=60`, `https://h/x?AWSAccessKeyId=AKIAIOSFODNN7EXAMPLE&Signature=@@`,
	// HTTP headers
	`X-Hub-Signature-256: sha256=@@`, `Signature: keyId="k",signature="@@"`, `x-amz-security-token: @@`, `Proxy-Authorization: Basic @@abc`,
	`api-key: @@`, `X-Api-Key=@@`, `Authorization => Bearer @@`, `cookie => sid=@@`, `Cookie:sid=@@`, `X-Session-Id: @@`,
	// YAML / TOML / INI / .netrc / properties
	"password: @@", "  - password: @@", `password = "@@"`, `secret_key = '@@'`, `[db] pass=@@`, `machine h login a password @@`,
	`login a password @@ machine x`, `spring.datasource.password=@@`, `jdbc.password=@@`, `smtp_password: @@`, `//registry.npmjs.org/:_authToken=@@`,
	`_authToken @@`, `_auth=@@`, `//npm.pkg.github.com/:_authToken @@`, `aws_secret_access_key=@@`, `aws_session_token=@@`,
	`apiKey: "@@"`, `access_key = @@`, `kubeconfig token: @@`, `client-key-data: @@`, `private-key-data: @@`, `bearerToken: @@`,
	// PHP / Ruby / Lua => forms and Makefile operators
	`'db_password' => '@@',`, `"secret" => "@@"`, `:password => "@@"`, `['token'] = '@@'`, `$password = '@@';`, `password => @@,`,
	`API_KEY ?= @@`, `SECRET := @@`, `export TOKEN ?= @@`, `DB_PASS ::= @@`, `GITHUB_TOKEN:=@@`, `PASSWORD?=@@`, `ACCESS_KEY ?= "@@"`,
	// CLI flags (docker / kubectl / aws / gcloud / az / helm / psql / mysql)
	`docker login -u a -p @@ registry`, `docker login --password @@`, `docker login --password-stdin @@`, `docker run -e DB_PASSWORD=@@ img`,
	`docker run -e API_TOKEN=@@ img`, `docker build --build-arg NPM_TOKEN=@@ .`, `kubectl create secret generic s --from-literal=password=@@`,
	`kubectl --token=@@ get pods`, `kubectl --token @@ get pods`, `aws configure set aws_secret_access_key @@`, `aws --secret-access-key @@ s3 ls`,
	`gcloud auth activate-service-account --key-file=k.json --password=@@`, `az login -u a --password @@`, `az login --password=@@`,
	`helm install x --set db.password=@@`, `helm upgrade --set auth.token=@@ x`, `terraform apply -var db_password=@@`,
	`terraform apply -var="api_token=@@"`, `psql --password=@@`, `mysql --password=@@`, `mysql --password @@ db`, `mongo --password @@`,
	`redis-cli --pass @@`, `redis-cli --pass @@`, `curl -H "X-Api-Key: @@" https://h`, `curl -H 'Authorization: Bearer @@' https://h`,
	`curl --oauth2-bearer @@ https://h`, `curl -u a:@@ https://h`, `wget --password=@@ https://h`, `wget --http-password=@@ https://h`,
	`ssh-keygen --passphrase @@`, `gpg --passphrase @@`, `vault login --token @@`, `vault write secret/x token=@@`, `git -c http.extraHeader="Authorization: Bearer @@" fetch`,
	`./run --db_pass @@`, `./run --admin-pass=@@`, `./run --user_pw @@`, `tool --client-secret=@@`, `tool --apiKey @@`, `tool --dbPass @@`,
	// DSNs and URLs
	`app:@@@tcp(db.example.com:3306)/x`, `user:@@@db.example.com:5432/x`, `root:@@@tcp(127.0.0.1:3306)/db`, `mysql://u:@@@h/db`,
	`redis://:@@@h:6379`, `amqp://guest:@@@h:5672/v`, `mongodb+srv://u:@@@c.mongodb.net/db`, `https://u:p@ss@@@h/`, `https://u:p@ssX@h/`,
	`postgres://u:p%40@@@h/db`, `ftp://a:@@@ftp.example.com/f`, `sftp://u:@@@h/p`, `https://user:@@@host.example.com`,
	`jdbc:mysql://h:3306/db?user=u&password=@@`, `Server=h;Database=d;User Id=u;Password=@@;`, `Server=h;Pwd=@@;`, `DefaultEndpointsProtocol=https;AccountKey=@@;`,
	`AccountKey=@@`, `LicenseKey=@@`, `license_key: @@`, `user:@@@example.org`, `//u:@@@h/x`, `totp=@@`, `TOTP: @@`, `hotp_secret=@@`,
	// cloud and vendor secrets
	`ghp_@@`, `gho_@@`, `github_pat_@@`, `xoxb-@@`, `xoxp-@@-1`, `xapp-1-@@`, `AKIA@@ABCD`, `ASIA@@ABCD`, `AIza@@abcd`, `ya29.@@abcdefgh`,
	`npm_@@`, `hf_@@abcdef`, `glpat-@@`, `sk-ant-@@`, `sk-proj-@@`, `using key rk_live_@@ here`, `webhook https://hooks.slack.com/services/T1/B2/@@ failed`,
	// escapes and unicode mixed with the new rules
	"pass​word => @@", "to​ken := @@", "Sig­nature: @@", "ｓｉｇ=@@", "db_pass = @@",
	`password   =>   "@@"`, "password\t=>\t@@", `"password"=>"@@"`, `password=>@@`,
	// wrapped error messages
	`request failed: {\"api_key\":\"@@\"}: status 401`, `GET https://h/x?sig=@@ returned 403`, `parse "https://u:@@@h/": invalid`,
	`dial tcp: lookup failed for root:@@@tcp(db.example.com:3306)/x`, `retry with --db-pass @@ failed`, `env: DB_PASS=@@: bad`,
})

// round3Payloads are structured payloads that must not retain the marker.
type round3Struct struct {
	DBPass   string `json:"db_pass"`
	MySQLPw  string `json:"mysql_pw"`
	Plain    string `json:"plain"`
	Salt     string `json:"salt"`
	Sig      string `json:"sig"`
	UserPass string `json:"userpass"`
	License  string `json:"license"`
	Key      string `json:"key"`
}

var round3Payloads = []any{
	map[string]any{"db_pass": round3Marker}, map[string]any{"DB_PASS": round3Marker}, map[string]any{"admin_pass": round3Marker},
	map[string]any{"mysql_pw": round3Marker}, map[string]any{"dbPass": round3Marker}, map[string]any{"DBPass": round3Marker},
	map[string]any{"adminPw": round3Marker}, map[string]any{"user-otp": round3Marker}, map[string]any{"userpass": round3Marker},
	map[string]any{"userpwd": round3Marker}, map[string]any{"key": round3Marker}, map[string]any{"sig": round3Marker},
	map[string]any{"salt": round3Marker}, map[string]any{"license": round3Marker}, map[string]any{"accountkey": round3Marker},
	map[string]any{"licensekey": round3Marker}, map[string]any{"account_key": round3Marker}, map[string]any{"oauth_state": round3Marker},
	map[string]any{"oauth": round3Marker}, map[string]any{"X-Goog-Signature": round3Marker},
	round3Struct{DBPass: round3Marker}, round3Struct{MySQLPw: round3Marker}, round3Struct{Salt: round3Marker}, round3Struct{Sig: round3Marker},
	round3Struct{UserPass: round3Marker}, round3Struct{License: round3Marker}, round3Struct{Key: round3Marker},
	[]any{"--db-pass", round3Marker}, []any{"--mysql-pw", round3Marker}, []any{"--admin_pass", round3Marker}, []any{"-dbPass", round3Marker},
	[]any{"-u", "admin:" + round3Marker}, []any{"--user", "admin:" + round3Marker}, []any{"--proxy-user", "p:" + round3Marker},
	[]any{"--user=a:" + round3Marker}, []any{"curl", "-u", "a:" + round3Marker, "https://h/"},
	[]string{"A_TOKEN=" + round3Marker}, []string{"DB_PASS=" + round3Marker, "x"}, []string{"--db-pass", round3Marker},
	map[string]any{"env": []any{map[string]any{"name": "DB_PASSWORD", "value": round3Marker}}},
	map[string]any{"containers": []any{map[string]any{"env": []any{map[string]any{"name": "API_TOKEN", "value": round3Marker}}}}},
	map[string]any{"name": "db_pass", "value": round3Marker}, map[string]any{"Name": "SECRET_KEY", "Value": round3Marker},
	map[string]any{"key": "Authorization", "value": "Bearer " + round3Marker}, map[string]any{"name": "x-api-key", "value": round3Marker},
	map[string]any{"name": "input_tokens", "value": round3Marker}, map[string]any{"name": "password", "value": []any{round3Marker}},
	map[string]any{"body": "{\"password\":\"" + round3Marker + "\"}"}, map[string]any{"log": `{\"password\":\"` + round3Marker + `\"}`},
	map[string]any{"cmd": []any{"mysql", "--password", round3Marker}}, map[string]any{"args": []any{"--db-pass=" + round3Marker}},
	map[string]any{"a": map[string]any{"b": map[string]any{"c": []any{"x", `{\"sig\":\"` + round3Marker + `\"}`}}}},
	map[string]any{"url": "https://h/x?sig=" + round3Marker}, map[string]any{"url": "https://u:" + round3Marker + "@h/"},
}

// round3Negatives are benign diagnostics that must survive unchanged.
var round3Negatives = []string{
	"author: alice", "authors: a, b", "authority granted", "authoritative answer", "author=alice", "authored by team",
	"signal received", "signal: killed", "signing key rotated", "design review", "assign owner", "signed off by bob", "sigma value 3",
	"significant change", "resigned", "consignment", "keyed lookup", "monkey patch applied", "turkey sandwich", "key rotation scheduled",
	"cache key computed", "sort key: name", "primary key exists", "bypass enabled", "compass north", "passenger count 3", "passes: 3", "passed",
	"image nginx:1.25@sha256:" + strings.Repeat("ab", 32), "ref app:v1@sha256:" + strings.Repeat("cd", 32),
	"git@github.com:a/b.git", "https://example.com/a?page=2&sort=asc", "https://example.com/p?q=sigma", "GET /v1/items?limit=10",
	"max 4096 tokens reached", "tokenizer ready", "docker run -u 1000 img", "ls -u", "salt lake city", "licensed under MIT", "seed value set",
	"usage: tool --verbose --count 3", "status=ok phase=validate", "elapsed 1.5s",
}

// round3NegativePayloads are structured payloads that must survive unchanged.
var round3NegativePayloads = []any{
	map[string]any{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
	map[string]any{"cache_read_input_tokens": 5, "cache_creation_input_tokens": 6, "input_tokens": 1, "output_tokens": 2},
	map[string]any{"reasoning_tokens": 3, "cached_tokens": 4, "max_tokens": 4096, "max_output_tokens": 8192},
	map[string]any{"author": "alice", "authors": []any{"a", "b"}, "authority": "root", "authoritative": true},
	map[string]any{"authorName": "alice", "author_email": "a@b.c"},
	map[string]any{"name": "REGION", "value": "us-east-1"}, map[string]any{"name": "x", "value": "y"},
	map[string]any{"vars": []any{map[string]any{"name": "LOG_LEVEL", "value": "debug"}}},
	map[string]any{"cache_key": "k", "sort_key": "n", "primary_key": "id", "partition": "p"},
}

func TestSanitizerRound3Positives(t *testing.T) {
	s := DefaultSanitizer()
	for _, in := range round3Positives {
		for _, v := range []any{in, map[string]any{"note": in}, []any{"a", in}} {
			got, san := sanitized(t, s, v)
			if containsMarker2(got) || san.Redactions == 0 {
				t.Errorf("%q leaked or was not counted: %s %+v", in, got, san)
			}
		}
	}
}

func containsMarker2(out string) bool {
	return strings.Contains(strings.ToLower(out), strings.ToLower(round3Marker))
}

func TestSanitizerRound3Payloads(t *testing.T) {
	s := DefaultSanitizer()
	for i, v := range round3Payloads {
		got, _ := sanitized(t, s, v)
		if containsMarker2(got) {
			b, _ := json.Marshal(v)
			t.Errorf("payload %d leaked: in %s out %s", i, b, got)
		}
	}
}

func TestSanitizerRound3Negatives(t *testing.T) {
	s := DefaultSanitizer()
	for _, in := range round3Negatives {
		want, _ := json.Marshal(in)
		if got, san := sanitized(t, s, in); got != string(want) || san.Redactions != 0 {
			t.Errorf("%q over-redacted: %s %+v", in, got, san)
		}
	}
	for i, v := range round3NegativePayloads {
		want, _ := json.Marshal(v)
		if got, san := sanitized(t, s, v); got != string(want) || san.Redactions != 0 {
			t.Errorf("payload %d over-redacted: %s", i, got)
		}
	}
}

func TestSanitizerRound3CorpusSizes(t *testing.T) {
	t.Logf("round-3 corpus: %d positive strings (x3 contexts), %d payloads, %d negative strings, %d negative payloads",
		len(round3Positives), len(round3Payloads), len(round3Negatives), len(round3NegativePayloads))
	if len(round3Positives) < 100+60 || len(round3Negatives) < 40 || len(round3Payloads) < 40 {
		t.Fatalf("round-3 corpus too small: %d %d %d", len(round3Positives), len(round3Negatives), len(round3Payloads))
	}
}

// adversarialDocs are structured shapes near the 1 MiB marshal cap.
func adversarialDocs() map[string]any {
	manyKeys := func(n int) map[string]any {
		m := make(map[string]any, n)
		for i := 0; i < n; i++ {
			m[fmt.Sprintf("k%02d", i)] = "v"
		}
		return m
	}
	grid := make([]any, 16) // 16 x 64 maps of 16 small keys (about 0.2 MiB marshalled)
	for i := range grid {
		row := make([]any, 64)
		for j := range row {
			row[j] = manyKeys(16)
		}
		grid[i] = row
	}
	long := make([]any, 12)
	for i := range long {
		long[i] = strings.Repeat("password is x ", 900)
	}
	return map[string]any{"grid-16x64x16": grid, "12-long-strings": long}
}

// TestSanitizerRound3Timing: adversarial 1 MiB inputs sanitize in bounded time
// (the budget is generous so slow CI cannot flake; a quadratic rule would take
// minutes). Timings are logged for the record (-v).
func TestSanitizerRound3Timing(t *testing.T) {
	s := DefaultSanitizer()
	const mib = 1 << 20
	inputs := map[string]string{
		"password-run":    strings.Repeat("password", mib/8),
		"escaped-quotes":  strings.Repeat(`\"`, mib/2),
		"backslash-run":   strings.Repeat(`\`, mib),
		"colon-at-run":    strings.Repeat("a:b@", mib/4),
		"dash-tokens":     strings.Repeat("--a ", mib/4),
		"user-flags":      strings.Repeat("-u x ", mib/5),
		"camel-run":       strings.Repeat("aPass", mib/5),
		"word-run":        strings.Repeat("a", mib),
		"sig-ampersand":   strings.Repeat("&sig", mib/4),
		"assign-ops":      strings.Repeat("token?=", mib/7),
		"slash-quote-mix": strings.Repeat(`\\\"/`, mib/5),
		"url-userinfo":    "://" + strings.Repeat("x", mib),
		"key-query":       strings.Repeat("?key", mib/4),
		"is-filler":       strings.Repeat("password is ", mib/12),
		"dot-dash-name":   strings.Repeat("a.-_", mib/4),
		"pairs-in-array":  strings.Repeat("--db-pass ", mib/10),
		"nonascii-mix":    strings.Repeat("\u00e9a ", mib/4),
		"hex-blob":        strings.Repeat("deadbeef", mib/8),
		"discord-slack":   strings.Repeat("hooks.slack.com/services/", mib/25),
		"dsn-candidates":  strings.Repeat("u:p@h.", mib/6),
	}
	check := func(name string, f func()) {
		start := time.Now()
		f()
		d := time.Since(start)
		if d > 10*time.Second {
			t.Errorf("%s took %s", name, d)
		}
		t.Logf("%s %s", name, d)
	}
	for name, in := range inputs {
		check(name, func() {
			if _, _, err := s.JSON(in); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, v := range adversarialDocs() {
		check(name, func() {
			if _, _, err := s.JSON(v); err != nil {
				t.Fatal(err)
			}
		})
	}
}
