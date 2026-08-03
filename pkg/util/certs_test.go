/*
Copyright 2022 The KubeZoo Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"io/ioutil"
	"math/big"
	"os"
	"testing"
	"time"

	"k8s.io/client-go/util/keyutil"
)

const (
	CA = `
-----BEGIN CERTIFICATE-----
MIICrDCCAZQCCQCfIou9gyjprjANBgkqhkiG9w0BAQUFADAYMRYwFAYDVQQDDA1L
VUJFUk5FVEVTLUNBMB4XDTIyMDYxODA2MzM0NFoXDTQ5MTEwMzA2MzM0NFowGDEW
MBQGA1UEAwwNS1VCRVJORVRFUy1DQTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCC
AQoCggEBAOg+yqx1ulz/atfl8Xhy6sBNUHdqBWmxsXOC5VLsqREB4Yl4rHILx2s5
fK/Pzv5+JSdXUym15nFc4RV2CnaMS6qGHn7bOQ7CVX57GE62a2CZvGWJVIwAuSia
bAqDid0HzDZ8pfpjzH4P4Kmh81PVOXdwXeGBqZ+FTo31ZjONg8Ae8+Q60d/P42fI
ri0NrHrlN9rdUFkouZHSj5Fo1tYU7IFxZYG3dMeHXLYv9XdLNt7YxrfFRKDSjKX+
DSrpG42KIk0oN1AJHUbWnP9xI/fp/pRmjqHb3W1rwa10oYNOa4GxrAHH439sqToi
6krQSQ6UwC4Q74oXOPTNClsOYnkiY4kCAwEAATANBgkqhkiG9w0BAQUFAAOCAQEA
Wm1C4BSEVxVcPY07UQGdRfV2yvimARI1tTvgEf2rVPvF9NtKuZzibmRo8q2usSwC
QbOO4l3YiWmxEb8jP97Oztf1RDAIXuPXNbt9sSoafZDFNPOBizdx7kqdQ+YdSOrt
r6Y9wjYBV8XN1pJO0QHFg8ZpyWmnEkZkwZw3MXj1sjiM+cg3vTc+sTzHAEppA217
b8p6AqiP7sS9jc1pIfRKYb0Z6psXp/HrN47V7wGfPFOdjBhUe4agqKnUFFduYM1Y
E7bB77ihNJx+XmmNlEUgOyiT+YhQlWP0KPSPUvPWGIXGTR75EaxKU/UZT7GMkP9i
GPF5l1PosqodQ2W1yEinLQ==
-----END CERTIFICATE-----`
	Key = `
-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA6D7KrHW6XP9q1+XxeHLqwE1Qd2oFabGxc4LlUuypEQHhiXis
cgvHazl8r8/O/n4lJ1dTKbXmcVzhFXYKdoxLqoYefts5DsJVfnsYTrZrYJm8ZYlU
jAC5KJpsCoOJ3QfMNnyl+mPMfg/gqaHzU9U5d3Bd4YGpn4VOjfVmM42DwB7z5DrR
38/jZ8iuLQ2seuU32t1QWSi5kdKPkWjW1hTsgXFlgbd0x4dcti/1d0s23tjGt8VE
oNKMpf4NKukbjYoiTSg3UAkdRtac/3Ej9+n+lGaOodvdbWvBrXShg05rgbGsAcfj
f2ypOiLqStBJDpTALhDvihc49M0KWw5ieSJjiQIDAQABAoIBAQDHpYLw8lt8qo0c
f60u0AsBuPTdUqTIkVpsZC/jM1K7LhTF6pjDiWCqykZnlIrqt2IVCbqR9q7c8O0F
V+3yrvQ06Qq6HpZUG7cG/aaNs79m0YHk/0NQ/yYsw2LxPtZ6zcM9a7X7I2OdUuTc
rj3Q6VF3XX825hH88cnvuu9ajeKeedoHGgUzDDgwqj8JvFbuHgTzHzmJ7wvdRxfn
5FaA1njrtKPEDzDNOB8LW/6xqTBr1p9zjwK+6CYLROvz9EjMvJfhcu7H/rsgYCbZ
mb2GIsWAKLU6XG50qakOUwpmJUjc8iKsON5WUMcAbzO8+sEXH5G+jfIPNxehof2H
i+0YSkFpAoGBAPdlRN/8ojiIJiSGFs7YdTpujvEnsqUUW2oQ/XiFEQRXA1mzTkTt
ZSi2GDDot5gpUsrt/YuK6gr0RKKLPmxFU7ow9gBIgn6Cxv6SuF63xx/SSzvCCQXy
cDH2Fe5rmcfnwNUouJJxA7YcNjtTp9yK0kUMPwY2EI2qpzUnMke7o5nXAoGBAPBS
oRRoa5ZnQya/XlIRZc54IPu856mvt70YGy7j0YYrQkWHtgEAZiTTZLA825sNEDlz
EcVKiGWK0Gv5TPc8e5BHZ+o48tpscBBz+KdxQ748s/WlCz9L+p4VoVkEGFtutGDk
nzID733ZTya1S36CbgpXH12fRP2kT8TIXDlm9AGfAoGAUmzIHMRkG+eopaSTNslB
jX1GXKx4Ra3ZoyYT/TKAb+y5rgoieq6JdJ3uw2TVvnmOHxRZ1EMtJQcrUuiHnLUg
ZzlmzMNbzuCtgiXKDay3SC/dZwSH0xZqMQsnVW8+Ji9dvOc7T3cd4G/X1b5SgBU0
Z1LkMKKUs053NSthAitPH7MCgYBed/y96vYv31O0TZGkLRaZ/PrqOi3OtDZD7M/y
tLdOSH76mghfiGqem0J/TMz+vDnee29G4K+RSun3J76riWkBJDCjD9PXLL04mn3q
REne5DnRnBk5voI71kDgnw18E55wYC58GLPyApRsoOOWTWs4QVshEFSsaAS7VA98
uQ29/QKBgCEKVg6auxmLcJIdZRYsaZme8UQDeFG+K40hYbDQGsxMmGUUGWJAd7+Z
tNf0B7NSMqe1z6HeRch4xSFaa7qOPyaeryualC3lLXCAHEV7OCuxcvY5e0bHU5PS
3FvKjLDyvO8Tx42Btz6So0DySqLfWljD1/1BaSLC64W7u4NlPqo9
-----END RSA PRIVATE KEY-----`
)

// TestNewTenantCertAndKey to ensure the NewTenantCertAndKey would return the desired ca/key.
func TestNewTenantCertAndKey(t *testing.T) {
	tenantId := "111111"

	caf, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatalf("error creating tmpfile: %v", err)
	}
	if _, err := keyutil.PrivateKeyFromFile(caf.Name()); err == nil {
		t.Fatalf("Expected error reading key from empty file, got none")
	}
	if err := ioutil.WriteFile(caf.Name(), []byte(CA), os.FileMode(0600)); err != nil {
		t.Fatalf("error writing ca to tmpfile: %v", err)
	}
	defer os.Remove(caf.Name())

	keyf, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatalf("error creating tmpfile: %v", err)
	}
	if _, err := keyutil.PrivateKeyFromFile(caf.Name()); err == nil {
		t.Fatalf("Expected error reading key from empty file, got none")
	}
	if err := ioutil.WriteFile(keyf.Name(), []byte(Key), os.FileMode(0600)); err != nil {
		t.Fatalf("error writing key to tmpfile: %v", err)
	}
	defer os.Remove(keyf.Name())

	tests := []struct {
		name             string
		caFile           string
		caKeyFile        string
		expectedCa       *x509.Certificate
		expectedKey      *rsa.PrivateKey
		expectedErrorNil bool
	}{
		{
			name:             "test with bad path ca files",
			caFile:           "a path not exists",
			caKeyFile:        "a path not exists",
			expectedErrorNil: false,
		},
		{
			name:             "test with path ca files",
			caFile:           caf.Name(),
			caKeyFile:        keyf.Name(),
			expectedErrorNil: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCa, gotKey, gotErr := NewTenantCertAndKey(test.caFile, test.caKeyFile, tenantId, 0)
			if test.expectedErrorNil && gotErr != nil {
				t.Errorf("expect nil error, got %s", gotErr)
				return
			}
			if !test.expectedErrorNil {
				if gotErr == nil {
					t.Errorf("expect error, got nil")
				}
				return
			}
			if gotCa.PublicKeyAlgorithm != x509.RSA {
				t.Errorf("unexpect SignatureAlgorithm")
			}
			if gotCa.Subject.OrganizationalUnit[0] != tenantId {
				t.Errorf("unexpect OU")
			}
			if gotCa.Subject.CommonName != tenantId+"-admin" {
				t.Errorf("unexpect CN")
			}
			if gotKey == nil {
				t.Errorf("unexpect nil key")
			}
		})
	}
}

// TestSignedCertValidity pins how long an issued certificate lives, and that it
// never claims to outlive the CA that signed it.
//
// ⭐ Validity is not cosmetic here. A client certificate cannot be revoked, so
// how long it is good for is the only bound there is on how long a credential
// keeps working after the platform would rather it stopped.
//
// ⚠️ The CA clamp had no test when it was written, and the lab could not have
// caught it either: the lab's CA lives five years, so nothing there ever issues
// a certificate long enough to hit the ceiling. This is the only place the case
// exists.
func TestSignedCertValidity(t *testing.T) {
	caKey, err := NewPrivateKey()
	if err != nil {
		t.Fatalf("generating a CA key: %v", err)
	}
	// A CA with a deliberately short life, which is the situation the clamp is
	// for: an issuer closer to its own expiry than the certificates it is being
	// asked to sign.
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(30 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, caTmpl, caTmpl, caKey.Public(), caKey)
	if err != nil {
		t.Fatalf("self-signing the CA: %v", err)
	}
	caCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing the CA: %v", err)
	}

	config := func(validity time.Duration) *Config {
		return &Config{
			CommonName:         "111111-admin",
			OrganizationalUnit: []string{"111111"},
			Usages:             []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			Validity:           validity,
		}
	}

	t.Run("honours the requested validity", func(t *testing.T) {
		key, err := NewPrivateKey()
		if err != nil {
			t.Fatalf("generating a key: %v", err)
		}
		cert, err := NewSignedCert(config(7*24*time.Hour), key, caCert, caKey)
		if err != nil {
			t.Fatalf("signing: %v", err)
		}
		want := time.Now().Add(7 * 24 * time.Hour)
		if cert.NotAfter.Sub(want) > time.Minute || want.Sub(cert.NotAfter) > time.Minute {
			t.Errorf("notAfter = %s, want about %s", cert.NotAfter, want)
		}
	})

	t.Run("clamps to the CA rather than outliving it", func(t *testing.T) {
		key, err := NewPrivateKey()
		if err != nil {
			t.Fatalf("generating a key: %v", err)
		}
		// Ten years against a CA with thirty days left.
		cert, err := NewSignedCert(config(10*365*24*time.Hour), key, caCert, caKey)
		if err != nil {
			t.Fatalf("signing: %v", err)
		}
		if cert.NotAfter.After(caCert.NotAfter) {
			t.Errorf("notAfter = %s, past the CA's %s. The certificate is not actually valid for "+
				"that extra time -- it only says it is, and whoever trusted the claim finds out "+
				"at a moment nobody planned for", cert.NotAfter, caCert.NotAfter)
		}
		if !cert.NotAfter.Equal(caCert.NotAfter) {
			t.Errorf("notAfter = %s, want the CA's own %s", cert.NotAfter, caCert.NotAfter)
		}
	})

	t.Run("zero validity falls back to the default", func(t *testing.T) {
		key, err := NewPrivateKey()
		if err != nil {
			t.Fatalf("generating a key: %v", err)
		}
		cert, err := NewSignedCert(config(0), key, caCert, caKey)
		if err != nil {
			t.Fatalf("signing: %v", err)
		}
		// The default is ten years, so against this CA it clamps -- which is the
		// point: a caller that sets nothing still cannot outlive the issuer.
		if !cert.NotAfter.Equal(caCert.NotAfter) {
			t.Errorf("notAfter = %s, want the CA's own %s", cert.NotAfter, caCert.NotAfter)
		}
	})
}
