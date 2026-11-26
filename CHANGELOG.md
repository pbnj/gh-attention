# Changelog

## [0.3.1](https://github.com/pbnj/gh-attention/compare/v0.3.0...v0.3.1) (2026-11-26)


### Bug Fixes

* **skill:** run inside a repo's checkout to load its rules ([4c5ed51](https://github.com/pbnj/gh-attention/commit/4c5ed51f4c556cef04f1af2a28a7bd88b23bb829))

## [0.3.0](https://github.com/pbnj/gh-attention/compare/v0.2.0...v0.3.0) (2026-11-26)


### ⚠ BREAKING CHANGES

* the requested section is shown by default, and -r/--reviews is removed. Leave `requested` out of -s or `sections:` to hide it.

### Features

* bundle an agent skill (gh attention skill) ([3f9f0c2](https://github.com/pbnj/gh-attention/commit/3f9f0c2ff3e58696221087be6b073eec39d37e8b))
* config file with rules that act on PRs; show review requests by default ([936f295](https://github.com/pbnj/gh-attention/commit/936f295c9a0e020034a3a5edb254d574736adf84))
* match rules by PR title ([946dbde](https://github.com/pbnj/gh-attention/commit/946dbde3dae3c099cea7a03457e7a096adc7d19f))

## [0.2.0](https://github.com/pbnj/gh-attention/compare/v0.1.0...v0.2.0) (2026-09-25)


### Features

* default to the current repo inside a git checkout ([#3](https://github.com/pbnj/gh-attention/issues/3)) ([dacdd75](https://github.com/pbnj/gh-attention/commit/dacdd753a1117e2fae221c65cf4dbe726219056e))
