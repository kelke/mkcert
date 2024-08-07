// Copyright 2018 The mkcert Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"net/mail"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

const defaultCountry string = "DE"

// Certificate validity must be less than 825 days,
// the limit that macOS/iOS apply to all certificates,
// See https://support.apple.com/en-us/HT210176.
//
// Leaf Certificate validity must be less than the root CA validity
var defaultLeafCertLifetime time.Time = time.Now().AddDate(1, 1, 0)
var defaultIntermediateLifetime time.Time = time.Now().AddDate(1, 1, 0)
var defaultRootLifetime time.Time = time.Now().AddDate(10, 0, 0)
var userFullName string
var defaultOrganization string
var reducedValidity bool = false

func init() {
	u, err := user.Current()
	if err == nil {
		userFullName = u.Name
		defaultOrganization = userFullName + " CA"
	}
}

func (m *mkcert) makeCert(hosts []string) {
	if m.caKey == nil {
		log.Fatalln("ERROR: can't create new certificates because the CA key (rootCA.key) is missing")
	}

	priv, err := m.generateKey(false)
	fatalIfErr(err, "failed to generate certificate key")
	pub := priv.(crypto.Signer).Public()

	expiration := m.getLifetime(defaultLeafCertLifetime, false)
	tpl := &x509.Certificate{
		SerialNumber: randomSerialNumber(),
		Subject: pkix.Name{
			CommonName: hosts[0],
		},

		NotBefore: time.Now(), NotAfter: expiration,

		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else if email, err := mail.ParseAddress(h); err == nil && email.Address == h {
			tpl.EmailAddresses = append(tpl.EmailAddresses, h)
		} else if uriName, err := url.Parse(h); err == nil && uriName.Scheme != "" && uriName.Host != "" {
			tpl.URIs = append(tpl.URIs, uriName)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}

	if m.client {
		tpl.ExtKeyUsage = append(tpl.ExtKeyUsage, x509.ExtKeyUsageClientAuth)
	}
	if len(tpl.IPAddresses) > 0 || len(tpl.DNSNames) > 0 || len(tpl.URIs) > 0 {
		tpl.ExtKeyUsage = append(tpl.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
	}
	if len(tpl.EmailAddresses) > 0 {
		tpl.ExtKeyUsage = append(tpl.ExtKeyUsage, x509.ExtKeyUsageEmailProtection)
	}

	// IIS (the main target of PKCS #12 files), only shows the deprecated
	// Common Name in the UI. See issue #115.
	if m.pkcs12 {
		tpl.Subject.CommonName = hosts[0]
	}

	cert, err := x509.CreateCertificate(rand.Reader, tpl, m.caCert, pub, m.caKey)
	fatalIfErr(err, "failed to generate certificate")

	certFile, keyFile, p12File := m.fileNames(hosts)

	if !m.pkcs12 {
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
		privDER, err := x509.MarshalPKCS8PrivateKey(priv)
		fatalIfErr(err, "failed to encode certificate key")
		privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})

		if certFile == keyFile {
			err = os.WriteFile(keyFile, append(certPEM, privPEM...), 0600)
			fatalIfErr(err, "failed to save certificate and key")
		} else {
			err = os.WriteFile(certFile, certPEM, 0644)
			fatalIfErr(err, "failed to save certificate")
			err = os.WriteFile(keyFile, privPEM, 0600)
			fatalIfErr(err, "failed to save certificate key")
		}
	} else {
		domainCert, _ := x509.ParseCertificate(cert)
		pfxData, err := pkcs12.Encode(rand.Reader, priv, domainCert, []*x509.Certificate{m.caCert}, "changeit")
		fatalIfErr(err, "failed to generate PKCS#12")
		err = os.WriteFile(p12File, pfxData, 0644)
		fatalIfErr(err, "failed to save PKCS#12")
	}

	m.printHosts(hosts)

	if !m.pkcs12 {
		if certFile == keyFile {
			log.Printf("\nThe certificate and key are at \"%s\" ✅\n\n", certFile)
		} else {
			log.Printf("\nThe certificate is at \"%s\" and the key at \"%s\" ✅\n\n", certFile, keyFile)
		}
	} else {
		log.Printf("\nThe PKCS#12 bundle is at \"%s\" ✅\n", p12File)
		log.Printf("\nThe legacy PKCS#12 encryption password is the often hardcoded default \"changeit\" ℹ️\n\n")
	}

	if reducedValidity {
		log.Printf("‼️ Reduced validity ‼️ %s 🗓\n\n", expiration.Format("2 January 2006"))
	} else {
		log.Printf("It will expire on %s 🗓\n\n", expiration.Format("2 January 2006"))
	}
}

func (m *mkcert) makeIntermediate() {
	if m.caKey == nil {
		log.Fatalln("ERROR: can't create new intermediate CA, because the root CA key (rootCA.key) is missing")
	}

	priv, err := m.generateKey(true)
	fatalIfErr(err, "failed to generate intermediate certificate key")
	pub := priv.(crypto.Signer).Public()

	spkiASN1, err := x509.MarshalPKIXPublicKey(pub)
	fatalIfErr(err, "failed to encode public key")

	var spki struct {
		Algorithm        pkix.AlgorithmIdentifier
		SubjectPublicKey asn1.BitString
	}
	_, err = asn1.Unmarshal(spkiASN1, &spki)
	fatalIfErr(err, "failed to decode public key")

	skid := sha1.Sum(spki.SubjectPublicKey.Bytes)

	expiration := m.getLifetime(defaultIntermediateLifetime, false)
	var cn string
	if m.interCN == "" {
		cn = defaultOrganization + " - Intermediate"
	} else {
		cn = m.interCN
	}

	tpl := &x509.Certificate{
		SerialNumber: randomSerialNumber(),
		Subject: pkix.Name{
			Country:            m.caCert.Subject.Country,
			Organization:       m.caCert.Subject.Organization,
			OrganizationalUnit: m.caCert.Subject.OrganizationalUnit,
			CommonName:         cn,
		},
		SubjectKeyId: skid[:],

		NotBefore: time.Now(), NotAfter: expiration,

		KeyUsage: x509.KeyUsageCertSign,

		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	interKeyName := cn + ".key"
	interCertName := cn + ".pem"
	cert, err := x509.CreateCertificate(rand.Reader, tpl, m.caCert, pub, m.caKey)
	fatalIfErr(err, "failed to generate intermediate certificate")

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	fatalIfErr(err, "failed to encode intermediate certificate key")
	err = os.WriteFile(interKeyName, pem.EncodeToMemory(
		&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0400)
	fatalIfErr(err, "failed to save intermediate certificate key")

	err = os.WriteFile(interCertName, pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: cert}), 0644)
	fatalIfErr(err, "failed to save intermediate certificate")

	log.Printf("Created a new intermediate certificate 🎉\n")

	log.Printf("\nThe certificate is at \"%s\" and the key at \"%s\" ✅\n\n", interCertName, interKeyName)

	if reducedValidity {
		log.Printf("‼️ Reduced validity ‼️ %s 🗓\n\n", expiration.Format("2 January 2006"))
	} else {
		log.Printf("It will expire on %s 🗓\n\n", expiration.Format("2 January 2006"))
	}
}

// Checks if leaf-/intermediate certificate validity is within the root validity
// reduces the requested validity to the validity of the root certificate if necessary
func validateExpiration(caCert *x509.Certificate, expiration time.Time) time.Time {
	if expiration.After(caCert.NotAfter) {
		expiration = caCert.NotAfter
		log.Println("The planned validity is longer than the root validity ⚠️")
		log.Println("Reducing validity to: ", expiration.Format("2 January 2006"), " ‼️")
		reducedValidity = true
	}
	return expiration
}

func (m *mkcert) printHosts(hosts []string) {
	secondLvlWildcardRegexp := regexp.MustCompile(`(?i)^\*\.[0-9a-z_-]+$`)
	log.Printf("\nCreated a new certificate valid for the following names 📜")
	for _, h := range hosts {
		log.Printf(" - %q", h)
		if secondLvlWildcardRegexp.MatchString(h) {
			log.Printf("   Warning: many browsers don't support second-level wildcards like %q ⚠️", h)
		}
	}

	for _, h := range hosts {
		if strings.HasPrefix(h, "*.") {
			log.Printf("\nReminder: X.509 wildcards only go one level deep, so this won't match a.b.%s ℹ️", h[2:])
			break
		}
	}
}

func (m *mkcert) generateKey(rootCA bool) (crypto.PrivateKey, error) {
	// root certificates should have a higher security level as their lifetime is considerably longer
	if rootCA {
		if m.rsa {
			return rsa.GenerateKey(rand.Reader, 4096)
		}
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	}

	if m.rsa {
		return rsa.GenerateKey(rand.Reader, 2048)
	}
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

func (m *mkcert) fileNames(hosts []string) (certFile, keyFile, p12File string) {
	defaultName := strings.Replace(hosts[0], ":", "_", -1)
	defaultName = strings.Replace(defaultName, "*", "_wildcard", -1)
	if len(hosts) > 1 {
		defaultName += "+" + strconv.Itoa(len(hosts)-1)
	}
	if m.client {
		defaultName += "-client"
	}

	certFile = "./" + defaultName + ".pem"
	if m.certFile != "" {
		certFile = m.certFile
	}
	keyFile = "./" + defaultName + ".key"
	if m.keyFile != "" {
		keyFile = m.keyFile
	}
	p12File = "./" + defaultName + ".p12"
	if m.p12File != "" {
		p12File = m.p12File
	}

	return
}

func randomSerialNumber() *big.Int {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	fatalIfErr(err, "failed to generate serial number")
	return serialNumber
}

func (m *mkcert) makeCertFromCSR() {
	if m.caKey == nil {
		log.Fatalln("ERROR: can't create new certificates because the CA key (rootCA.key) is missing")
	}

	csrPEMBytes, err := os.ReadFile(m.csrPath)
	fatalIfErr(err, "failed to read the CSR")
	csrPEM, _ := pem.Decode(csrPEMBytes)
	if csrPEM == nil {
		log.Fatalln("ERROR: failed to read the CSR: unexpected content")
	}
	if csrPEM.Type != "CERTIFICATE REQUEST" &&
		csrPEM.Type != "NEW CERTIFICATE REQUEST" {
		log.Fatalln("ERROR: failed to read the CSR: expected CERTIFICATE REQUEST, got " + csrPEM.Type)
	}
	csr, err := x509.ParseCertificateRequest(csrPEM.Bytes)
	fatalIfErr(err, "failed to parse the CSR")
	fatalIfErr(csr.CheckSignature(), "invalid CSR signature")

	expiration := m.getLifetime(defaultLeafCertLifetime, false)
	tpl := &x509.Certificate{
		SerialNumber:    randomSerialNumber(),
		Subject:         csr.Subject,
		ExtraExtensions: csr.Extensions, // includes requested SANs, KUs and EKUs

		NotBefore: time.Now(), NotAfter: expiration,

		// If the CSR does not request a SAN extension, fix it up for them as
		// the Common Name field does not work in modern browsers. Otherwise,
		// this will get overridden.
		DNSNames: []string{csr.Subject.CommonName},

		// Likewise, if the CSR does not set KUs and EKUs, fix it up as Apple
		// platforms require serverAuth for TLS.
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	if m.client {
		tpl.ExtKeyUsage = append(tpl.ExtKeyUsage, x509.ExtKeyUsageClientAuth)
	}
	if len(csr.EmailAddresses) > 0 {
		tpl.ExtKeyUsage = append(tpl.ExtKeyUsage, x509.ExtKeyUsageEmailProtection)
	}

	cert, err := x509.CreateCertificate(rand.Reader, tpl, m.caCert, csr.PublicKey, m.caKey)
	fatalIfErr(err, "failed to generate certificate")
	c, err := x509.ParseCertificate(cert)
	fatalIfErr(err, "failed to parse generated certificate")

	var hosts []string
	hosts = append(hosts, c.DNSNames...)
	hosts = append(hosts, c.EmailAddresses...)
	for _, ip := range c.IPAddresses {
		hosts = append(hosts, ip.String())
	}
	for _, uri := range c.URIs {
		hosts = append(hosts, uri.String())
	}
	certFile, _, _ := m.fileNames(hosts)

	err = os.WriteFile(certFile, pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: cert}), 0644)
	fatalIfErr(err, "failed to save certificate")

	m.printHosts(hosts)

	log.Printf("\nThe certificate is at \"%s\" ✅\n\n", certFile)

	log.Printf("It will expire on %s 🗓\n\n", expiration.Format("2 January 2006"))
}

// loadOrGenerateCA will load or create the CA at CAROOT.
func (m *mkcert) loadOrGenerateCA() {
	if !pathExists(filepath.Join(m.CAROOT, rootName)) || m.forceNewRoot {
		m.newCA()
	}

	certPEMBlock, err := os.ReadFile(filepath.Join(m.CAROOT, rootName))
	fatalIfErr(err, "failed to read the CA certificate")
	certDERBlock, _ := pem.Decode(certPEMBlock)
	if certDERBlock == nil || certDERBlock.Type != "CERTIFICATE" {
		log.Fatalln("ERROR: failed to read the CA certificate: unexpected content")
	}
	m.caCert, err = x509.ParseCertificate(certDERBlock.Bytes)
	fatalIfErr(err, "failed to parse the CA certificate")
	if m.caCert.NotAfter.Before(time.Now()) {
		log.Fatalln("ERROR: Your Root certificate has expired, pass -root to generate a new one!")
	}

	if !pathExists(filepath.Join(m.CAROOT, rootKeyName)) {
		return // keyless mode, where only -install works
	}

	keyPEMBlock, err := os.ReadFile(filepath.Join(m.CAROOT, rootKeyName))
	fatalIfErr(err, "failed to read the CA key")
	keyDERBlock, _ := pem.Decode(keyPEMBlock)
	if keyDERBlock == nil || keyDERBlock.Type != "PRIVATE KEY" {
		log.Fatalln("ERROR: failed to read the CA key: unexpected content")
	}
	m.caKey, err = x509.ParsePKCS8PrivateKey(keyDERBlock.Bytes)
	fatalIfErr(err, "failed to parse the CA key")
}

func (m *mkcert) newCA() {
	priv, err := m.generateKey(true)
	fatalIfErr(err, "failed to generate the CA key")
	pub := priv.(crypto.Signer).Public()

	spkiASN1, err := x509.MarshalPKIXPublicKey(pub)
	fatalIfErr(err, "failed to encode public key")

	var spki struct {
		Algorithm        pkix.AlgorithmIdentifier
		SubjectPublicKey asn1.BitString
	}
	_, err = asn1.Unmarshal(spkiASN1, &spki)
	fatalIfErr(err, "failed to decode public key")

	skid := sha1.Sum(spki.SubjectPublicKey.Bytes)

	var country []string
	if m.rootCountry == "" {
		country = []string{defaultCountry}
	} else {
		country = []string{m.rootCountry}
	}

	var org []string
	if m.rootOrg == "" {
		org = []string{defaultOrganization}
	} else {
		org = []string{m.rootOrg}
	}

	var orgUnit []string
	if m.rootOU != "" {
		orgUnit = []string{m.rootOU}
	}

	var cn string
	if m.rootCN == "" {
		cn = userFullName + " - "
		if m.rsa {
			cn += "RSA"
		} else {
			cn += "ECC"
		}
		cn += " Root"
	} else {
		cn = m.rootCN
	}

	expiration := m.getLifetime(defaultRootLifetime, true)
	tpl := &x509.Certificate{
		SerialNumber: randomSerialNumber(),
		Subject: pkix.Name{
			Country:            country,
			Organization:       org,
			OrganizationalUnit: orgUnit,
			// The CommonName is required by iOS to show the certificate in the
			// "Certificate Trust Settings" menu.
			// https://github.com/FiloSottile/mkcert/issues/47
			CommonName: cn,
		},
		SubjectKeyId: skid[:],

		NotBefore: time.Now(), NotAfter: expiration,

		KeyUsage: x509.KeyUsageCertSign,

		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            2,
	}

	cert, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	fatalIfErr(err, "failed to generate CA certificate")

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	fatalIfErr(err, "failed to encode CA key")

	renamePath := filepath.Join(m.CAROOT, rootKeyName)
	if pathExists(renamePath) {
		newPath := renamePath + "-old.bak"
		err = os.Rename(renamePath, newPath)
		fatalIfErr(err, "failed to move old root CA key")
		log.Println("Moved old root CA key to " + newPath + " ➡️")
	}
	renamePath = filepath.Join(m.CAROOT, rootName)
	if pathExists(renamePath) {
		// move old certificate to <oldname>-old.bak
		newPath := renamePath + "-old.bak"
		err = os.Rename(renamePath, newPath)
		fatalIfErr(err, "failed to move old root CA certificate")
		log.Println("Moved old root CA certificate to " + newPath + " ➡️")
	}
	err = os.WriteFile(filepath.Join(m.CAROOT, rootKeyName), pem.EncodeToMemory(
		&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0400)
	fatalIfErr(err, "failed to save CA key")

	err = os.WriteFile(filepath.Join(m.CAROOT, rootName), pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: cert}), 0644)
	fatalIfErr(err, "failed to save CA certificate")

	log.Printf("Created a new local CA 💥\n")
}

func (m *mkcert) caUniqueName() string {
	return defaultOrganization + " - RootCA" + m.caCert.SerialNumber.String()
}

// returns a valid not.After date
func (m *mkcert) getLifetime(defaultLifetime time.Time, isRoot bool) time.Time {
	lifetime := defaultLifetime

	if isRoot {
		if m.rootYears != 0 {
			lifetime = time.Now().AddDate(m.rootYears, 0, 0)
		}
		// root certificates don't need lifetime validation
		return lifetime
	}

	if m.days != 0 || m.months != 0 || m.years != 0 {
		lifetime = time.Now().AddDate(m.years, m.months, m.days)
	}
	return validateExpiration(m.caCert, lifetime)
}
