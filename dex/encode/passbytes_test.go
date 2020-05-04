package encode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

const (
	digits   = "0123456789"
	specials = "~=+%^*/()[]{}/!@#$?|\"\\&-_<>',.;"
	all      = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" + "abcdefghijklmnopqrstuvwxyz" + digits + specials
)

func randomString(length int) string {
	if length == 0 {
		return ""
	}

	shuffle := func(b []byte) string {
		rand.Shuffle(len(b), func(i, j int) {
			b[i], b[j] = b[j], b[i]
		})
		return string(b)
	}

	rand.Seed(time.Now().UnixNano())
	buf := make([]byte, length)

	if length <= 2 {
		for i := 0; i < length; i++ {
			buf[i] = all[rand.Intn(len(all))] // include additional random char from `all`
		}
		return shuffle(buf)
	}

	buf[0] = digits[rand.Intn(len(digits))]     // include at least 1 digit
	buf[1] = specials[rand.Intn(len(specials))] // include at least 1 special char
	for i := 2; i < length; i++ {
		buf[i] = all[rand.Intn(len(all))] // include additional random char from `all`
	}

	return shuffle(buf)
}

// TestMarshalUnmarshal generates random strings and ensures that, with the
// exception of strings containing invalid chars such as `\xe2`,
// - each string can be marshalled directly into a JSON-encoded byte slice,
// - the JSON-encoded byte slice can be unmarshalled into a `PassBytes` ptr,
// - the string representation of the `PassBytes` is the same as the original
//   string.
// TestMarshalUnmarshal also creates a test object with each string
// and ensures that
// - the test object can be marshalled into a JSON-encoded byte slice,
// - the JSON-encoded byte slice can be unmarshalled into another object having
//   a `PassBytes` field,
// - the string value of the second object's PassBytes field is the same as the
//   original string.
// TestMarshalUnmarshal also ensures that marshalling strings with invalid chars
// produces an error.
func TestMarshalUnmarshal(t *testing.T) {
	checkDirectUnmarshal := func(password string, expectMarshalError bool) error {
		// marshal to json first
		pwJSON, err := json.Marshal(password)
		if err != nil {
			return err
		}
		// unmarshal back from json
		var pwFromJSON PassBytes
		if err = json.Unmarshal(pwJSON, &pwFromJSON); err != nil {
			return err
		}
		// confirm accuracy of unmarshal
		unmarshalledPassword := string(pwFromJSON)
		if password != unmarshalledPassword {
			return fmt.Errorf("original pw: %q marshalled to %q unmarshalled to %q",
				password, string(pwJSON), unmarshalledPassword)
		}
		return nil
	}

	checkUnmarshalAsBodyProperty := func(password string, expectMarshalError bool) error {
		// marshal to json first
		originalObj := struct {
			Password string `json:"pass"`
		}{
			Password: password,
		}
		jsonObj, err := json.Marshal(originalObj)
		if err != nil {
			return err
		}
		// unmarshal back from json
		var restoredObj = new(struct {
			Password PassBytes `json:"pass"`
		})
		if err = json.Unmarshal(jsonObj, restoredObj); err != nil {
			return err
		}
		// confirm accuracy of unmarshal
		unmarshalledPassword := string(restoredObj.Password)
		if password != unmarshalledPassword {
			return fmt.Errorf("original obj: %v (password %q) marshalled to %s and unmarshalled to %v (password %q)",
				originalObj, password, string(jsonObj), restoredObj, unmarshalledPassword)
		}
		return nil
	}

	// test with 100 randomly generated strings of length between 0 and 19 chars
	for i := 0; i < 100; i++ {
		s := randomString(i % 20)
		expectMarshalError := !json.Valid([]byte(s)) // expect marshal error if s contains invalid chars
		if err := checkDirectUnmarshal(s, expectMarshalError); err != nil {
			t.Fatal(err)
		}
		if err := checkUnmarshalAsBodyProperty(s, expectMarshalError); err != nil {
			t.Fatal(err)
		}
	}
}

// jsonString represents a JSON-encoded string, which may or may not be valid.
type jsonString string

// bytes returns `js.jsonPass` converted to []byte.
func (js jsonString) bytes() []byte {
	return []byte(js)
}

// string attempts to unmarshal `js.jsonPass` into a string. Returns the
// unmarshalled string or an error if unmarshalling fails.
func (js jsonString) string() (s string, err error) {
	err = json.Unmarshal(js.bytes(), &s)
	return s, err
}

// TestUnmarshalJSONEncodedStrings defines a collection of valid and invalid
// JSON-encoded strings (aka `jsonString`s).
// Each jsonString is converted to JSON-encoded data using `[]byte(s)` and
// unmarshalled into a `PassBytes` ptr.
// Ensures successful unmarshalling for valid JSON-encoded strings and errors
// for invalid JSON-encoded strings.
func TestUnmarshalJSONEncodedStrings(t *testing.T) {
	// Test with pre-defined inputs and expectations. Inputs will be converted
	// to bytes (not json-marshalled), so we'd expect inputs that are not valid
	// json-encoded strings to fail.
	expectionsTests := []struct {
		name      string
		jsonPass  jsonString
		expectErr bool
	}{
		// valid json-encoded strings
		{
			name:      "empty string",
			jsonPass:  `""`,
			expectErr: false,
		},
		{
			name:      "qutf-8 string",
			jsonPass:  `"@123"`,
			expectErr: false,
		},
		{
			name:      "string with escaped char (\")",
			jsonPass:  `"quote\"embedded"`,
			expectErr: false,
		},
		{
			name:      "unicode character",
			jsonPass:  `"\u5f5b"`,
			expectErr: false,
		},
		{
			name:      "unicode surrogate pair",
			jsonPass:  `"\uD800\uDC00"`,
			expectErr: false,
		},
		{
			name:      "valid non-utf8 characters",
			jsonPass:  `"√ç∂"`,
			expectErr: false,
		},

		// invalid json strings
		{
			name:      "unquoted empty string",
			jsonPass:  "",
			expectErr: true,
		},
		{
			name:      "unquoted UTF8 string",
			jsonPass:  `@123`,
			expectErr: true,
		},
		{
			name:      `quoted string with unescaped " char`,
			jsonPass:  `"quote"embedded"`,
			expectErr: true,
		},
		{
			name:      `invalid unicode character (\U instead of \u)`,
			jsonPass:  `"\UD800"`,
			expectErr: true,
		},
		{
			name:      `invalid unicode surrogate pair (\U instead of \u)`,
			jsonPass:  `"\UD800\UDC00"`,
			expectErr: true,
		},
	}

	for _, test := range expectionsTests {
		var unmarshalledPassBytes PassBytes
		err := json.Unmarshal(test.jsonPass.bytes(), &unmarshalledPassBytes)
		if test.expectErr && err != nil {
			continue
		}
		if test.expectErr {
			t.Fatalf("%s -> %q: expected error but got nil", test.name, test.jsonPass)
		}
		if err != nil {
			t.Fatalf("%s -> %q: unexpected unmarshal error: %v", test.name, test.jsonPass, err)
		}

		// If we got here, we expect unmarshalling to have been successful,
		// confirm that the unmarshalled password is accurate.
		actualPassword, err := test.jsonPass.string()
		if err != nil {
			t.Fatalf("%s: unexpected error getting actual password string from JSON-encoded string (%q): %v",
				test.name, test.jsonPass, err)
		}
		unmarshalunmarshalledPassword := string(unmarshalledPassBytes)
		if actualPassword != unmarshalunmarshalledPassword {
			t.Fatalf("%s: expected %q, got %q", test.name, actualPassword, unmarshalunmarshalledPassword)
		}
	}
}

// TestMarshal ensures that `PassBytes` can be properly marshalled into raw
// bytes which can be unmarshalled back into a `PassBytes` ptr either directly
// or as part of a struct object.
func TestMarshal(t *testing.T) {
	pb := PassBytes("def")
	pbRawBytes, err := json.Marshal(pb)
	if err != nil {
		t.Fatalf("cannot marshal PassBytes: %v", err)
	}

	// check if pbRawBytes can be successfully unmarshalled into *PassBytes
	var pbUnmarshalled PassBytes
	err = json.Unmarshal(pbRawBytes, &pbUnmarshalled)
	if err != nil {
		t.Fatalf("cannot unmarshal the rawbytes of a PassBytes: %v", err)
	}

	if !bytes.Equal(pb, pbUnmarshalled) {
		t.Fatalf("expected original passbytes %x to equal unmarshalled passbytes %s", pb, pbUnmarshalled)
	}
}
