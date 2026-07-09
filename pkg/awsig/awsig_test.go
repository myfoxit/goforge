package awsig

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAWSDocumentedVector checks the canonical example from the AWS SigV4
// documentation (GET iam ListUsers, 2015-08-30).
func TestAWSDocumentedVector(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")

	creds := Credentials{
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
	}
	ts, _ := time.Parse(timeFormat, "20150830T123600Z")
	if err := Sign(req, nil, creds, "us-east-1", "iam", ts); err != nil {
		t.Fatal(err)
	}
	auth := req.Header.Get("Authorization")
	wantSig := "5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if !strings.Contains(auth, "Signature="+wantSig) {
		t.Fatalf("signature mismatch:\n%s\nwant ...Signature=%s", auth, wantSig)
	}
	if !strings.Contains(auth, "SignedHeaders=content-type;host;x-amz-date") {
		t.Fatalf("signed headers wrong: %s", auth)
	}
}

func TestURIEncode(t *testing.T) {
	if got := uriEncode("a b/c~d"); got != "a%20b%2Fc~d" {
		t.Fatalf("uriEncode = %s", got)
	}
	if got := uriEncodePath("/bucket/my%20file.txt"); got != "/bucket/my%20file.txt" {
		t.Fatalf("uriEncodePath = %s", got)
	}
}
