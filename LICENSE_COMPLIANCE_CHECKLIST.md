# License and provider-terms compliance checklist

Complete this checklist for every release candidate and retain the resulting evidence with release artifacts.

## Source and dependency inventory

- [ ] CycloneDX SBOMs exist for all five release images (four product workloads plus OAuth2 Proxy) and identify direct, transitive, OS and build dependencies by exact version.
- [ ] Go module and npm lockfiles are committed and agree with the SBOM; the build did not resolve floating versions.
- [ ] Container base images and copied binaries have a documented license and source location.
- [ ] The pinned OAuth2 Proxy image/version and its transitive image contents have been scanned; its Apache-2.0 notices and redistribution obligations are included in the release notice inventory.
- [ ] Automated license policy reports contain no unreviewed `UNKNOWN`, unlicensed, SSPL, BUSL or strong-copyleft dependency.
- [ ] Any reciprocal-license dependency has a written compatibility analysis covering linking, distribution and source obligations.

## Notices and distribution

- [ ] Required copyright notices, license texts and attribution are present in the distributed web assets/container notice bundle.
- [ ] Source-offer or relinking obligations, if any, are satisfied for the exact artifact.
- [ ] Generated assets, icons, fonts, translations and map-related visual assets have provenance and redistribution rights.
- [ ] Project license headers and repository license are consistent with third-party obligations.
- [ ] SBOM, provenance and notices are attached to the release and retained for the supported-version period.

## Yandex and other provider terms

- [ ] The current official Router, JavaScript Maps, Geocoder/Geosuggest documentation and commercial terms were reviewed by an accountable owner on the release date.
- [ ] The contracted license covers the deployed geography, traffic, request volume, alternative routes, waypoints and production purpose.
- [ ] Server and browser credentials are separate, restricted to the documented APIs/origins/IPs and not sublicensed or disclosed.
- [ ] `PROVIDER_DATA_STORAGE_ALLOWED`, `PROVIDER_DATA_MODIFICATION_ALLOWED` and `ENABLE_PROVIDER_CACHE` match the contracted storage, modification, caching and retention rights.
- [ ] Raw responses, traffic tiles and undocumented endpoints are absent from storage, telemetry, fixtures and product logic.
- [ ] Required map/provider attribution and branding are displayed without implying that Пробок.Нет's congestion classification is an official Yandex traffic level.
- [ ] Provider references/geometries are returned, transformed and retained only as explicitly permitted.
- [ ] Failure/load testing uses a permitted isolated quota and does not violate provider rate or acceptable-use terms.

## Release approval evidence

- [ ] CI license and vulnerability gates passed for the exact commit and image digests.
- [ ] Exceptions identify component/version, license, business rationale, legal/security approver, compensating action and expiry.
- [ ] Security, privacy and product owners approved any new data source, provider, cache, analytics or asset.
- [ ] The release record names the reviewer and includes the completed date, SBOM digests, image digests and terms/version references.

A checked box records verification, not an assumption. When documentation and implementation disagree, the provider terms and applicable law control; disable the affected capability until the discrepancy is resolved.
