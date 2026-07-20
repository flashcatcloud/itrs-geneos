# README Branding and Integration Key Design

Date: 2026-07-20

## Objective

Update the repository's user-facing README files so the product name is consistently written as `Flashduty` and users can obtain and configure an `integration_key` without guessing.

## Scope

Only `README.md` and `README.zh-CN.md` will change. Application code, configuration behavior, historical design documents, release workflows, and existing release artifacts remain unchanged.

## Considered Approaches

1. **README-only update (selected):** correct the two current entry-point documents and add concise setup instructions. This directly addresses the user-facing problem without changing runtime behavior.
2. **Repository-wide terminology rewrite:** also modify source messages and historical design records. This is rejected because it broadens the change and rewrites historical material unnecessarily.
3. **Link-only update:** add the official URL without summarizing the steps. This is rejected because users would still need to leave the README to understand how to obtain the key.

## Documentation Changes

All product-name occurrences in the two README files change from `FlashDuty` to `Flashduty`. Code identifiers such as `FLASHDUTY_INTEGRATION_KEY`, executable names, URLs, and runtime behavior remain unchanged. Troubleshooting text may paraphrase a runtime error instead of quoting its old brand spelling literally.

Both README files will explain the two official ways to obtain a push URL:

- dedicated integration: open a collaboration space, select **Integration Data**, add a **Standard Alert Event** integration, save it, open the generated card, and copy the push URL;
- shared integration: open **Integration Center → Alert Events**, create a **Standard Alert Event** integration, configure its default route, save it, and copy the generated push URL.

The documentation will explain that `integration_key` is the query parameter in that push URL. Users may copy its value into `flashduty.integration_key` in YAML or set `FLASHDUTY_INTEGRATION_KEY`. It will link to:

`https://docs.flashduty.com/zh/on-call/integration/alert-integration/alert-sources/standard-alert`

## Validation

- search both README files to ensure no `FlashDuty` spelling remains;
- confirm `Flashduty`, `integration_key`, and the official documentation URL appear in both documents;
- verify Markdown links and code blocks remain well formed;
- inspect the final diff to confirm no non-README runtime changes are included.
