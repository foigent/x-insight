//go:build !integration

package db_test

import (
	"crypto/x509"
	"net/http"
	"testing"
)

func runXIManagedJoint(t *testing.T, _ urJointConsumerInput, _ *x509.CertPool, _ *http.Client) {
	t.Fatal("managed joint helper requires the existing integration build tag")
}
