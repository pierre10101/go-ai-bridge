package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const releaseFixture = "testdata/good/release_example"

// mutateRelease copies the release fixture, applies from -> to on action.go
// (and on queries/ when sqlFrom is set) and renders it.
func mutateRelease(t *testing.T, from, to string) (string, error) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(releaseFixture, "action.go"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(moduleTempDir(t), "release_example")
	copyDir(t, releaseFixture, dir)
	if from != "" {
		if !strings.Contains(string(src), from) {
			t.Fatalf("fixture action.go no longer contains %q", from)
		}
		if err := os.WriteFile(filepath.Join(dir, "action.go"), []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Render(dir)
}

// TestSessionContract (T2): the session is listed with the values the
// server sets, quoting httpx.SessionRule, and never among the body fields
// the caller sends; the 400 line says why a request that sends it fails; the
// steps call it "the session from the cookie".
func TestSessionContract(t *testing.T) {
	got, err := Render(releaseFixture)
	if err != nil {
		t.Fatal(err)
	}
	body, _, ok := strings.Cut(got, "Every field is required")
	if !ok {
		t.Fatalf("no input rule in:\n%s", got)
	}
	if strings.Contains(body, "`session`") || !strings.Contains(body, "with this field and no others:\n- `seat_id`: a whole number.\n") {
		t.Fatalf("session is listed as a body field:\n%s", body)
	}
	for _, want := range []string{
		"The action also takes this value, which the caller does not send:\n- `session`: set by the server from the session cookie `bridge_session`: its value when that is a whole number from 1 up, written in digits only, or 0 when the request has no such cookie, has it more than once, or its value is anything else; the caller does not send it, and a request that does is answered with HTTP 400 below.\n",
		"or is larger than 1 MiB; or the request sends a value that the server sets, in the body or in the query string. The action does not run.",
		"1. If the session from the cookie is at most 0, stop with F1",
		"`held_by` is the session from the cookie at that moment",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	// Two server-set values: "these 2 values".
	got, err = mutateRelease(t, "\tSession int64 `json:\"session\" server:\"session\"`\n", "\tSession int64 `json:\"session\" server:\"session\"`\n\tNow     int64 `json:\"now\" clock:\"now\"`\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := "The action also takes these 2 values, which the caller does not send:\n- `session`: set by the server from the session cookie"; !strings.Contains(got, want) {
		t.Fatalf("want %q in:\n%s", want, got)
	}
}

// TestSessionRules: server-set fields of other shapes are refused (T2).
func TestSessionRules(t *testing.T) {
	const field = "Session int64 `json:\"session\" server:\"session\"`"
	cases := map[string]struct{ from, to, want string }{
		"other server value": {field, "Session int64 `json:\"session\" server:\"admin\"`",
			"refused: server field Session tagged server:\"admin\" is not in the allowed pattern list (T2 session is passed in)"},
		"bool session": {field, "Session bool `json:\"session\" server:\"session\"`",
			"refused: server field Session of type bool is not in the allowed pattern list (T2 session is passed in)"},
		"on output": {"SeatID int64 `json:\"seat_id\"`\n}\n\nvar", "SeatID int64 `json:\"seat_id\" server:\"session\"`\n}\n\nvar",
			"refused: server tag on SeatID outside Input is not in the allowed pattern list (T2 session is passed in)"},
		"also clock": {field, "Session int64 `json:\"session\" clock:\"now\" server:\"session\"`",
			"refused: field Session tagged both clock and server"},
		"from the query": {field, "Session int64 `json:\"session\" query:\"session\" server:\"session\"`",
			"refused: server field Session with a path or query tag"},
		"two sessions": {field, field + "\n\tOther int64 `json:\"other\" server:\"session\"`",
			"refused: second session field Other"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := mutateRelease(t, tc.from, tc.to)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in:\n%v", tc.want, err)
			}
		})
	}
	t.Run("text session", func(t *testing.T) {
		got, err := mutateRelease(t, field, "Session string `json:\"session\" server:\"session\"`")
		if err != nil {
			t.Fatal(err)
		}
		want := "- `session`: set by the server from the session cookie `bridge_session`: its value when that is 1 to 128 letters, digits, '-', '_' or '.', or the empty text when the request has no such cookie"
		if !strings.Contains(got, want) {
			t.Fatalf("want %q in:\n%s", want, got)
		}
	})
}

// TestRollbackWording pins what a stopping step says about an earlier write
// (see wroteSome, wroteMaybe, wroteNone in handle.go).
func TestRollbackWording(t *testing.T) {
	got, err := Render(releaseFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// a read right after the claim: the count is not known yet
		"3. Read: count the seats whose `id` is the request's `seat_id` (query `CountSeats` in queries/seat_after.sql). If the query fails, stop with HTTP 500 Internal Server Error.\n   Any change made in step 2 is rolled back.\n",
		// a guard that stops only when the claim changed nothing
		"stop with F2: HTTP 404 Not Found \"seat does not exist\".\n   Nothing was written in step 2, so there is nothing to roll back.\n",
		// the S10 catch-all: 0 rows or several
		"stop with F3: HTTP 409 Conflict \"seat is not held by this session\".\n   Any change made in step 2 is rolled back.\n",
		// after "exactly one row changed": the write happened
		"If any of these is false, it is a bug: stop with HTTP 500 Internal Server Error.\n   The write in step 2 is rolled back.\n",
		"- F1 \"session is required\": HTTP 401 Unauthorized; step 1, before any write.",
		"- F2 \"seat does not exist\": HTTP 404 Not Found; step 4, after a write that changed nothing, so nothing was written.",
		"- F3 \"seat is not held by this session\": HTTP 409 Conflict; step 6, after a write that may have changed rows; any change it made is rolled back.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "F2: HTTP 404 Not Found \"seat does not exist\".\n   Any change") ||
		strings.Contains(got, "F2: HTTP 404 Not Found \"seat does not exist\".\n   The write") {
		t.Errorf("F2 (no seat was changed) says a write is rolled back:\n%s", got)
	}

	// Once "not exactly one" has stopped the action, a later read knows the
	// claim changed its row; "if released == 0 { stop }" tells the same.
	for name, guard := range map[string]string{"!= 1": "released != 1", "== 0": "released == 0"} {
		t.Run("after "+name, func(t *testing.T) {
			from := "\tseats, err := a.q.CountSeats(ctx, in.SeatID)"
			got, err := mutateRelease(t, from, "\tif "+guard+" {\n\t\treturn Output{}, F3\n\t}\n"+from)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"3. If " + map[string]string{"!= 1": "not exactly one", "== 0": "no"}[name] + " seat was changed in step 2, stop with F3",
				"4. Read: count the seats whose `id` is the request's `seat_id` (query `CountSeats` in queries/seat_after.sql). If the query fails, stop with HTTP 500 Internal Server Error.\n   The write in step 2 is rolled back.\n",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("want %q in:\n%s", want, got)
				}
			}
		})
	}

	// An insert (Q3) always wrote its row: "The write in step N is rolled back."
	inv, err := Render("testdata/good/create_invoice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inv, "The write in step 5 is rolled back.") || strings.Contains(inv, "Any change made") || strings.Contains(inv, "Nothing was written") {
		t.Errorf("insert rollback wording:\n%s", inv)
	}
}
