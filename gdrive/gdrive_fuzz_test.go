// A fuzz target for the one function here that parses bytes off the wire.
//
// parseTokenResponse is handed an HTTP status and a response body from
// accounts.google.com — or from whatever a misconfigured api_base_url points
// at, which is the interesting case. It is the closest thing this repository
// has to the Rust drivers' on-disk format parsers: untrusted bytes in, a
// decision out, and the decision decides whether the driver tells the caller
// to re-authenticate.
//
// THE PROPERTY WORTH FUZZING IS NOT "IT DOES NOT PANIC". It is the one the
// function's own comment claims and a substring match would break:
//
//	The reauth detection inspects the structured `error` field only; a
//	substring match would false-positive when "invalid_grant" appears
//	inside `error_description`.
//
// A false "re-authenticate" is a driver that throws away a working refresh
// token because a server put a phrase in a human-readable string. That is the
// statement asserted below, over any body a fuzzer can build.

package gdrive

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzParseTokenResponse(f *testing.F) {
	f.Add(200, []byte(`{"access_token":"ya29.a0"}`))
	f.Add(200, []byte(`{}`))
	f.Add(200, []byte(`not json`))
	f.Add(400, []byte(`{"error":"invalid_grant"}`))
	f.Add(400, []byte(`{"error":"invalid_client","error_description":"the invalid_grant you sent"}`))
	f.Add(401, []byte(``))
	f.Add(500, []byte(`{"error":{"nested":"invalid_grant"}}`))
	f.Add(200, []byte(`{"access_token":null}`))
	f.Add(0, []byte(`{"error":"invalid_grant"}`))

	f.Fuzz(func(t *testing.T, status int, body []byte) {
		token, err := parseTokenResponse(status, body)

		// A non-200 is never a token, whatever the body says.
		if status != 200 {
			if err == nil {
				t.Fatalf("parseTokenResponse(%d, %q) returned no error", status, body)
			}
			if token != "" {
				t.Fatalf("parseTokenResponse(%d, %q) returned a token %q on a failure",
					status, body, token)
			}
		}

		// A 200 that does not parse is an error; a 200 that does is whatever
		// access_token held, including the empty string.
		if status == 200 && err != nil && token != "" {
			t.Fatalf("parseTokenResponse(200, %q) returned both %q and %v", body, token, err)
		}

		// THE ONE THAT MATTERS. Re-authentication is demanded only when the
		// STRUCTURED error field is exactly "invalid_grant". Anything else —
		// the phrase inside a description, inside a nested object, inside the
		// token itself — must not trigger it.
		if err == nil {
			return
		}
		saysReauth := strings.Contains(err.Error(), "oauth_reauth_required")
		if !saysReauth {
			return
		}
		var probe struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &probe) != nil || probe.Error != "invalid_grant" {
			t.Fatalf("parseTokenResponse(%d, %q) demanded re-authentication, but the "+
				"structured error field is not exactly \"invalid_grant\" — a working "+
				"refresh token would have been thrown away", status, body)
		}
	})
}
