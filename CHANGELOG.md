# Changelog

## 1.0.0 (2026-07-15)


### Features

* **BSOD-68:** Minimax Token Plan provider plugin ([#33](https://github.com/danstis/ai-usage-dashboard/issues/33)) ([79ff676](https://github.com/danstis/ai-usage-dashboard/commit/79ff67683ce7c5dfcdc3ef93c9492346cd41ba82))
* **BSOD-73:** OpenAPI 3 /api/v1 contract + config bootstrap ([dad81dd](https://github.com/danstis/ai-usage-dashboard/commit/dad81dd53c4dc6c09ad8507af41682c4d3aaf0f5))
* **BSOD-74:** HTTP API skeleton — router, structured errors, request logging ([bb598cf](https://github.com/danstis/ai-usage-dashboard/commit/bb598cfffe131cb2e51b0e20cc88dd96ab070bd8))
* **BSOD-75:** add SQLite persistence with repository interfaces ([adff013](https://github.com/danstis/ai-usage-dashboard/commit/adff013dad9b14354a6e851c4b1baa0213c66c5d))
* **BSOD-75:** SQLite persistence — migrations + repository interfaces ([11c58f9](https://github.com/danstis/ai-usage-dashboard/commit/11c58f9d7dc8e0f4db58191134e0fd99f61b5d80))
* **BSOD-76:** provider registry + enable/disable endpoints ([820259e](https://github.com/danstis/ai-usage-dashboard/commit/820259e46d4c625628b95f4e7ff2a2c7d66f15b9))
* **BSOD-78:** add Swagger UI at /docs ([607390b](https://github.com/danstis/ai-usage-dashboard/commit/607390bc6ab5b7c594438b57ea5e386c9a5df6e9))
* **BSOD-78:** add Swagger UI at /swaggerui ([74f0e9e](https://github.com/danstis/ai-usage-dashboard/commit/74f0e9ef3c91d6890c8e1907e19f5d7005f346d6))
* **BSOD-80:** executable provider contract ([1164dfe](https://github.com/danstis/ai-usage-dashboard/commit/1164dfe1b35d2e30fbb97a110a95754abb84123e))
* **BSOD-81:** credential crypto core (AES-256-GCM) ([27a60bd](https://github.com/danstis/ai-usage-dashboard/commit/27a60bd8909bef95ee2c4df7ba7f4d1daa8f3897))
* **BSOD-82:** secure credential store + write-only credential API ([#25](https://github.com/danstis/ai-usage-dashboard/issues/25)) ([0de2a26](https://github.com/danstis/ai-usage-dashboard/commit/0de2a26c75f32458a5dd26ac7df321446adfa42e))
* **BSOD-84:** usage snapshot store + read API ([#27](https://github.com/danstis/ai-usage-dashboard/issues/27)) ([c870bf4](https://github.com/danstis/ai-usage-dashboard/commit/c870bf44ce1834aea8220ee66ca74a9ad0867a47))
* **BSOD-85:** background scheduler + on-demand refresh (P2/S5) ([#28](https://github.com/danstis/ai-usage-dashboard/issues/28)) ([09ed02b](https://github.com/danstis/ai-usage-dashboard/commit/09ed02bb49a6a9b92bba3a0ff4caea3eb29f88d1))
* **BSOD-90:** P3.0 shared foundation for provider plugins ([#32](https://github.com/danstis/ai-usage-dashboard/issues/32)) ([b0d7bf9](https://github.com/danstis/ai-usage-dashboard/commit/b0d7bf9ec1a96fff9aef42080e9749bef00e28db))
* scaffold Go service, CI/CD and healthz (BSOD-59) ([b5689ef](https://github.com/danstis/ai-usage-dashboard/commit/b5689ef22ebf35b9b4b6e357105e9c69ee8dbf1e))


### Bug Fixes

* add Sonar config to BSOD-59 CI ([6bc7f24](https://github.com/danstis/ai-usage-dashboard/commit/6bc7f241500f9e2dd00c07a820c99cbc1367cb6e))
* **BSOD-78:** add SRI integrity to Swagger UI CDN assets ([8fb155a](https://github.com/danstis/ai-usage-dashboard/commit/8fb155a8883375640a4bfe86178139bdd3ee4572))
* **deps:** update module github.com/getkin/kin-openapi to v0.142.0 ([66e82e7](https://github.com/danstis/ai-usage-dashboard/commit/66e82e795674201f4fc4d3530ccaf0a1a09af5d1))
* pin golangci-lint-action to v2-compatible version, bump actions ([7e5fd4b](https://github.com/danstis/ai-usage-dashboard/commit/7e5fd4b7e93c7367807c26d3cc6b15d6162f679c))
