
# Supply Chain Security

Users of the evroc CSI may want to verfiy the security of the artefacts delivered.
All Docker images and Helm charts are signed with [Cosign](https://github.com/sigstore/cosign) using keyless signing via GitHub OIDC. This ensures the authenticity and integrity of released artifacts.

evroc takes security very seriously, and as such we provide:

- **Signed Images**: All images and charts are cryptographically signed with Cosign
- **SBOM**: Software Bill of Materials (SPDX format) attached to each release
- **SLSA Provenance**: Build provenance attestations for supply chain transparency
- **Pinned Actions**: GitHub Actions are pinned to commit SHAs to prevent supply chain attacks

## Pre-requestite

You will need `cosign` installed.
Follow https://docs.sigstore.dev/cosign/installation/ to download it.

## Verify the cryptographic signature and github identity of the tgz ball

```bash
# Download chart and signature files
VERSION=0.1.6
wget https://github.com/evroc-oss/evroc-csi-driver/releases/download/v${VERSION}/evroc-csi-driver-${VERSION}.tgz
wget https://github.com/evroc-oss/evroc-csi-driver/releases/download/v${VERSION}/evroc-csi-driver-${VERSION}.tgz.sig
wget https://github.com/evroc-oss/evroc-csi-driver/releases/download/v${VERSION}/evroc-csi-driver-${VERSION}.tgz.pem

# Verify the signature (keyless)
cosign verify-blob evroc-csi-driver-${VERSION}.tgz \
  --signature evroc-csi-driver-${VERSION}.tgz.sig \
  --certificate evroc-csi-driver-${VERSION}.tgz.pem \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"

# Install the verified chart
helm install evroc-csi-driver ./evroc-csi-driver-${VERSION}.tgz \
  --namespace kube-system \
  --set evroc.existingConfigSecret=evroc-credentials
```

## Image and Chart Signing

All Docker images and Helm charts are signed with [Cosign](https://github.com/sigstore/cosign) using keyless signing via GitHub OIDC. This ensures the authenticity and integrity of released artifacts.

### Verify Docker Image Signature

Install Cosign and verify the image signature:

```bash
# Install Cosign (if not already installed)
# See: https://docs.sigstore.dev/cosign/installation/

# Verify image signature (keyless)
cosign verify \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/evroc-oss/evroc-csi-driver:latest
```

### Verify Helm Chart Signature

```bash
# Verify Helm chart signature (keyless)
cosign verify \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  oci://ghcr.io/evroc-oss/evroc-csi-driver:latest
```

### Verify SBOM Attestation

```bash
# Verify and view SBOM attestation
cosign verify-attestation \
  --type spdxjson \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/evroc-oss/evroc-csi-driver:latest
```

**Extract SBOM for analysis:**

The SBOM is attached as an attestation to the container image. To extract it for vulnerability scanning or license compliance analysis:

```bash
# Extract SBOM to a file
cosign verify-attestation \
  --type spdxjson \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/evroc-oss/evroc-csi-driver:latest \
  | jq -r '.payload' | base64 -d | jq -r '.predicate' > sbom.spdx.json
```

You can then analyze the SBOM with vulnerability scanners like [Grype](https://github.com/anchore/grype), [Trivy](https://github.com/aquasecurity/trivy), or [Bomber](https://github.com/devops-kung-fu/bomber):

```bash
# Example: Scan for vulnerabilities using Grype
grype sbom:./sbom.spdx.json
```

### Verify SLSA Provenance

```bash
# Verify and view SLSA provenance attestation
cosign verify-attestation \
  --type slsaprovenance \
  --certificate-identity-regexp="^https://github.com/evroc-oss/evroc-csi-driver/" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/evroc-oss/evroc-csi-driver:latest \
  | jq -r '.payload' | base64 -d | jq
```

This shows the build provenance including the source repository, commit SHA, builder identity, and build timestamps.