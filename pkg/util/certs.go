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
	"crypto"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"net"
	"time"

	"github.com/pkg/errors"
)

const (
	AnnotationTenantKubeConfigBase64 = "kubezoo.io/tenant.kubeconfig.base64"
	// AnnotationTenantCredentialIssuedAt records when a tenant was last issued
	// credentials, in RFC3339.
	//
	// ⭐ It is what makes "issued once, not kept" expressible at all. The
	// kubeconfig annotation carries the tenant's PRIVATE KEY, and the platform
	// has no reason to hold a copy of every tenant's key forever -- one read of
	// one object would be all of them. But an annotation that has simply been
	// deleted cannot be told apart from one that was never written, so a
	// controller that cleared it would reissue on the next reconcile and the key
	// would be straight back.
	//
	// With this marker the three states are distinct: nothing issued yet
	// (neither annotation present), issued and still being collected (both), and
	// issued then withdrawn (only this one). Removing this annotation is
	// therefore the gesture that asks for a new credential -- which puts the
	// timing where it belongs, with whoever wants the credential, rather than
	// having the platform rotate on the tenant's behalf.
	AnnotationTenantCredentialIssuedAt = "kubezoo.io/tenant.credential-issued-at"
	// AnnotationTenantCredentialExpiresAt records when the credential last issued
	// to a tenant stops working, in RFC3339.
	//
	// ⭐ Written down because otherwise it cannot be seen. The certificate carries
	// its own NotAfter, but reading it means base64-decoding an annotation and
	// running openssl over the result -- so in practice the first anyone hears of
	// an expired credential is a 401 from CI on a morning nobody picked. A
	// shorter validity is only workable if expiry is a routine, visible event
	// rather than a surprise, and this is what makes it visible.
	//
	// ⚠️ Kept when the stored kubeconfig is withdrawn. Which credential a tenant
	// holds is not knowable from the platform side once the copy is gone, so this
	// is the only remaining answer to "when does theirs stop working".
	AnnotationTenantCredentialExpiresAt = "kubezoo.io/tenant.credential-expires-at"

	KubeZooClusterName = "kube-zoo"

	RsaKeySize = 2048
	// CertificateValidity is the fallback validity for a signed certificate: 10
	// years.
	//
	// ⚠️ Long, and inherited rather than chosen. It stays the fallback only so
	// that callers which do not set Config.Validity keep behaving as they did;
	// anything issuing tenant credentials should be picking a number on purpose.
	// A client certificate cannot be revoked, so its validity is the only bound
	// there is on how long a credential keeps working after the platform would
	// rather it did not. For comparison, kubeadm issues one year and the public
	// CAs will not go past 398 days.
	CertificateValidity = time.Hour * 24 * 365 * 10
)

// Config contains the basic fields required for creating a certificate
type Config struct {
	CommonName         string
	Organization       []string
	OrganizationalUnit []string
	AltNames           AltNames
	Usages             []x509.ExtKeyUsage
	// Validity is how long the certificate is good for. Zero or less means
	// CertificateValidity.
	Validity time.Duration
}

// AltNames contains the domain names and IP addresses that will be added
// to the API Server's x509 certificate SubAltNames field. The values will
// be passed directly to the x509.Certificate object.
type AltNames struct {
	DNSNames []string
	IPs      []net.IP
}

// EncodeCertPEM returns PEM-endcoded certificate data.
func EncodeCertPEM(cert *x509.Certificate) []byte {
	block := pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return pem.EncodeToMemory(&block)
}

// EncodePrivateKeyPEM returns PEM-encoded private key data.
func EncodePrivateKeyPEM(key *rsa.PrivateKey) []byte {
	block := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}

	return pem.EncodeToMemory(&block)
}

// NewTenantCertAndKey creates new certificate and key for the denoted tenant.
// validity of zero or less means CertificateValidity.
func NewTenantCertAndKey(caFile, caKeyFile, tenantID string, validity time.Duration) (*x509.Certificate, *rsa.PrivateKey, error) {
	// load ca, ca-key from files
	tlsCert, err := tls.LoadX509KeyPair(caFile, caKeyFile)
	if err != nil {
		return nil, nil, err
	}

	key, ok := tlsCert.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, nil, fmt.Errorf("private key is not crypto.Signer")
	}
	cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse cert: %v", err)
	}
	// generate the certificate config
	config := &Config{
		OrganizationalUnit: []string{tenantID},
		CommonName:         tenantID + "-admin",
		Usages:             []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		Validity:           validity,
	}

	return NewCertAndKey(cert, key, config)
}

// NewCertAndKey creates new certificate and key by passing the certificate authority certificate and key.
func NewCertAndKey(caCert *x509.Certificate, caKey crypto.Signer, config *Config) (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := NewPrivateKey()
	if err != nil {
		return nil, nil, errors.Wrap(err, "unable to create private key")
	}

	cert, err := NewSignedCert(config, key, caCert, caKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "unable to sign certificate")
	}

	return cert, key, nil
}

// NewPrivateKey creates an RSA private key.
func NewPrivateKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(cryptorand.Reader, RsaKeySize)
}

// NewSignedCert creates a signed certificate using the given CA certificate and key.
func NewSignedCert(cfg *Config, key crypto.Signer, caCert *x509.Certificate, caKey crypto.Signer) (*x509.Certificate, error) {
	serial, err := cryptorand.Int(cryptorand.Reader, new(big.Int).SetInt64(math.MaxInt64))
	if err != nil {
		return nil, err
	}
	if len(cfg.CommonName) == 0 {
		return nil, errors.New("must specify a CommonName")
	}
	if len(cfg.Usages) == 0 {
		return nil, errors.New("must specify at least one ExtKeyUsage")
	}
	// OrganizationalUnit is required by Tenant authentication
	if len(cfg.OrganizationalUnit) == 0 {
		return nil, errors.New("must specify a OrganizationalUnit")
	}

	// ⭐ How long this certificate lives is the caller's to choose, and it matters
	// more here than the default suggests. A client certificate cannot be
	// revoked: once handed over it is good until it expires, so its validity IS
	// the platform's only bound on how long a withdrawn or disputed credential
	// keeps working. Ten years is a default nobody chose, not a decision.
	validity := cfg.Validity
	if validity <= 0 {
		validity = CertificateValidity
	}
	notAfter := time.Now().Add(validity).UTC()
	// ⚠️ Never outlive the CA that signed it. A certificate whose NotAfter is
	// past its issuer's is not valid for that extra time -- it just claims to be,
	// and the failure lands on whoever trusted the claim, at a moment nobody
	// planned for.
	if notAfter.After(caCert.NotAfter) {
		notAfter = caCert.NotAfter
	}
	certTmpl := x509.Certificate{
		Subject: pkix.Name{
			CommonName:         cfg.CommonName,
			Organization:       cfg.Organization,
			OrganizationalUnit: cfg.OrganizationalUnit,
		},
		DNSNames:     cfg.AltNames.DNSNames,
		IPAddresses:  cfg.AltNames.IPs,
		SerialNumber: serial,
		NotBefore:    caCert.NotBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  cfg.Usages,
	}
	certDERBytes, err := x509.CreateCertificate(cryptorand.Reader, &certTmpl, caCert, key.Public(), caKey)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(certDERBytes)
}
