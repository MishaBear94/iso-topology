# Maintaining the official documentation

The official site is **https://iso-topology.mintlify.site/**. It uses this
repository's `docs/` directory directly. There is no
second copy of the reference documentation and no separate documentation repo.
`docs.json` defines navigation and branding; `index.mdx`, `quickstart.mdx`, and
`gallery.mdx` are the site's entry pages. Existing Markdown pages remain the
shared source for GitHub, agents, and the published site.

## Preview and verify

Use Node.js 22 or later and the CLI version pinned in the documentation workflow:

```bash
npm install --global mint@4.2.808
cd docs
mint validate --telemetry false
mint broken-links --check-anchors --check-redirects --telemetry false
mint dev --no-open --port 3000 --telemetry false
```

Open `http://localhost:3000`. Check the overview, quickstart, a reference page,
the icon table, and the gallery. Inspect both light and dark mode and a narrow
viewport. Local search can differ from the deployed search index.

These docs describe `main`. Quickstart installs `@main` so its commands match
the current source. `@latest` selects the newest tagged release and may lag
behind these docs. Do not change Go import paths when the GitHub repository
owner changes: the module is still `github.com/MarkovWangRR/iso-topology`.

## Generated content

Run from the repository root after changing the capability catalog or samples:

```bash
go run ./tools/gen-docs
```

This writes the capability reference, icon catalog and previews, sample index,
JSON Schema, and root `llms.txt`, and replaces the generated section of the
prompt template. Change the generator instead of editing its output. Keep the
prompt template's Markdown reference markers intact.

Mintlify parses `.md` as MDX too. Close image tags, include alt text, escape
literal pipes in tables (including inside inline code), and avoid splitting
inline code spans across lines. Generated prose must escape MDX expressions
and HTML-like placeholders. Keep relative `.md` links between shared pages so
they also work on GitHub. Links outside `docs/` must use full repository URLs.

The Documentation workflow checks generated-file drift, build warnings, broken
internal links, heading anchors, and redirects. External links are reviewed
separately because availability and rate limits can make them intermittent.

## Hosting and deployment

Use Mintlify's free **Starter** plan and its assigned free subdomain. A paid
plan, custom domain, and credit card are not needed for the initial site.
Confirm the current plan in the dashboard before completing onboarding.

The **iso-topology** project is connected with these settings:

| Setting | Value |
|---|---|
| Repository | `MishaBear94/iso-topology` |
| Branch | `main` |
| Documentation directory | `docs` |
| Configuration | `docs/docs.json` |
| Public URL | `https://iso-topology.mintlify.site/` |
| Mintlify organization / project | `sponson` / `iso-topology` |

If installing the Mintlify GitHub App, grant access only to this repository.
Do not replace an existing documentation project. With the repository
connection active, Mintlify deploys changes from `main`; no deploy secret is
needed in GitHub Actions. Mintlify can deploy independently of the validation
workflow, so check the workflow before treating a change as verified. Use branch
protection requiring the Documentation check if maintainers want enforcement.

For each published change:

1. Check the Documentation workflow and the deployment in the
   [Mintlify activity dashboard](https://app.mintlify.com/sponson/iso-topology/activity).
2. Check the overview, quickstart, gallery,
   [schema download](https://iso-topology.mintlify.site/agent/schema/dsl.schema.json), and sample links on
   the public URL. Check search after indexing completes.
3. Verify that the deployment corresponds to the intended Git commit.
4. Verify that the plan remains Starter and no paid add-on is enabled.

The first hosted deployment succeeded on **2026-10-12**, using documentation
from commit `756ea0c9a3b4e0adfa7802ad3a9ead73032bcc4d`. The 27 navigation pages,
`sitemap.xml`, `llms.txt`, and `llms-full.txt` are publicly accessible. The root
READMEs link to the official site.

The dashboard's current plan is **Starter ($0/month)**. An existing promotional
Pro trial is separate from the site's hosting requirement; do not upgrade or
make the documentation depend on trial-only features.

## Recovery and later changes

Fix a failed docs build in the same source files, rerun validation, and push the
fix. To undo a published change, revert its commit and let Mintlify redeploy.
Keep the last successful deployment available while investigating a failure.

`design/`, `testing.md`, and this maintainer guide are excluded by `.mintignore`.
They stay in the public repository but are not product documentation pages.
Keep future internal engineering notes out of the published navigation.

The Cloudflare website and any custom domain are separate follow-up work. The
documentation site can run on its free subdomain without either dependency.
