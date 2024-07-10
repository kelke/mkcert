# mkcert

mkcert is a simple tool for making locally-trusted development certificates. It requires no configuration.

## Fork Details
This is a fork of [FiloSottile's mkcert](https://github.com/FiloSottile/mkcert) with more personal Subject Names.  
The following default Subject Values cannot be changed, which is why this fork was created:

- Organization: `mkcert development certificate`
- Organizational Unit: `<username@fqdn> (<Full-Name>)`
- Common Name: `mkcert <username@fqdn> (<Full-Name>)`



This fork changes these Subject Values to the following:

- Country: `<Countycode>`
- Organization: `<Full-Name>`
- Organizational Unit: `username@hostname - mkcert`
- Common Name: `<Full-Name> - RootCA`



## Description

```
$ mkcert -install
Created a new local CA 💥
The local CA is now installed in the system trust store! ⚡️
The local CA is now installed in the Firefox trust store (requires browser restart)! 🦊

$ mkcert example.com "*.example.com" example.test localhost 127.0.0.1 ::1

Created a new certificate valid for the following names 📜
 - "example.com"
 - "*.example.com"
 - "example.test"
 - "localhost"
 - "127.0.0.1"
 - "::1"

The certificate is at "./example.com+5.crt" and the key at "./example.com+5.key" ✅
```

<p align="center"><img width="498" alt="Chrome and Firefox screenshot" src="https://user-images.githubusercontent.com/1225294/51066373-96d4aa80-15be-11e9-91e2-f4e44a3a4458.png"></p>

Using certificates from real certificate authorities (CAs) for development can be dangerous or impossible (for hosts like `example.test`, `localhost` or `127.0.0.1`), but self-signed certificates cause trust errors. Managing your own CA is the best solution, but usually involves arcane commands, specialized knowledge and manual steps.

mkcert automatically creates and installs a local CA in the system root store, and generates locally-trusted certificates. mkcert does not automatically configure servers to use the certificates, though, that's up to you.

## Installation

> **Warning**: the `rootCA.key` file that mkcert automatically generates gives complete power to intercept secure requests from your machine. Do not share it.

### Build from source (requires Go 1.13+)

```
git clone https://github.com/FiloSottile/mkcert && cd mkcert
go build -ldflags "-X main.Version=$(git describe --tags)"
```

This fork also includes a build script for some major systems.  
Run with:
```
build/build.sh
```
from project root

## Supported root stores

mkcert supports the following root stores:

* macOS system store
* Windows system store
* Linux variants that provide either
    * `update-ca-trust` (Fedora, RHEL, CentOS) or
    * `update-ca-certificates` (Ubuntu, Debian, OpenSUSE, SLES) or
    * `trust` (Arch)
* Firefox (macOS and Linux only)
* Chrome and Chromium
* Java (when `JAVA_HOME` is set)

To only install the local root CA into a subset of them, you can set the `TRUST_STORES` environment variable to a comma-separated list. Options are: "system", "java" and "nss" (includes Firefox).

## Advanced topics

### Advanced options

```
	-cert-file FILE, -key-file FILE, -p12-file FILE
	    Customize the output paths.

	-client
	    Generate a certificate for client authentication.

	-rsa
	    Generate a certificate with an RSA key (RSA-2048 for Leaf, RSA-4096 for Root).

	-pkcs12
	    Generate a ".p12" PKCS #12 file, also know as a ".pfx" file,
	    containing certificate and key for legacy applications.

	-csr CSR
	    Generate a certificate based on the supplied CSR. Conflicts with
	    all other flags and arguments except -install and -cert-file.
```

> **Note:** You _must_ place these options before the domain names list.

#### Example

```
mkcert -key-file key.crt -cert-file cert.crt example.com *.example.com
```

### S/MIME

mkcert automatically generates an S/MIME certificate if one of the supplied names is an email address.

```
mkcert filippo@example.com
```

### Mobile devices

For the certificates to be trusted on mobile devices, you will have to install the root CA. It's the `rootCA.crt` file in the folder printed by `mkcert -CAROOT`.

On iOS, you can either use AirDrop, email the CA to yourself, or serve it from an HTTP server. After opening it, you need to [install the profile in Settings > Profile Downloaded](https://github.com/FiloSottile/mkcert/issues/233#issuecomment-690110809) and then [enable full trust in it](https://support.apple.com/en-nz/HT204477).

For Android, you will have to install the CA and then enable user roots in the development build of your app. See [this StackOverflow answer](https://stackoverflow.com/a/22040887/749014).

### Using the root with Node.js

Node does not use the system root store, so it won't accept mkcert certificates automatically. Instead, you will have to set the [`NODE_EXTRA_CA_CERTS`](https://nodejs.org/api/cli.html#cli_node_extra_ca_certs_file) environment variable.

```
export NODE_EXTRA_CA_CERTS="$(mkcert -CAROOT)/rootCA.crt"
```

### Changing the location of the CA files

The CA certificate and its key are stored in an application data folder in the user home. You usually don't have to worry about it, as installation is automated, but the location is printed by `mkcert -CAROOT`.

If you want to manage separate CAs, you can use the environment variable `$CAROOT` to set the folder where mkcert will place and look for the local CA files.

### Installing the CA on other systems

Installing in the trust store does not require the CA key, so you can export the CA certificate and use mkcert to install it in other machines.

* Look for the `rootCA.crt` file in `mkcert -CAROOT`
* copy it to a different machine
* set `$CAROOT` to its directory
* run `mkcert -install`

Remember that mkcert is meant for development purposes, not production, so it should not be used on end users' machines, and that you should *not* export or share `rootCA.key`.
