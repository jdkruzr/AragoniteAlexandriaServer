# Brand names and asset

Aragonite Alexandria is the client; Aragonite Alexandria Server is the server.
Aragonite Alexandria Hosting is the separate commercial hosting control plane.
The Server uses the shared Aragonite crystal with a Greek library facade.

![Aragonite Alexandria crystal and Greek library](../assets/aragonite-alexandria-server-crystal-temple-v1.png)

The canonical raster mark is
`assets/aragonite-alexandria-server-crystal-temple-v1.png`. It is used by the README
and exported into the embedded web header and browser favicon. Run
`python3 assets/export-icons.py` (requires Pillow) to regenerate the web sizes.

It was generated with the built-in image generation tool using the shared crystal
master and the Alexandria client's Greek library silhouette as references. The
[full prompt](../assets/aragonite-alexandria-server-crystal-temple-v1.txt) records
the composition. This is a generated derivative, not pixel-exact layering.

The reusable [crystal master](../assets/aragonite-crystal-master-v1.png) and its
[source prompt](../assets/aragonite-crystal-master-v1.txt) are retained locally,
identical to the copies in PowerSearch, PowerSync and Alexandria Client. Warm
amber/ivory facets, muted teal secondary symbols and charcoal outlines connect
the family. The earlier woven-page Server mark is superseded.

The PNG is 1254×1254 with transparency. Derivative sizes should be generated
from this canonical file rather than repeatedly recompressing another export.

## Web UI

The web UI follows the Broadsheet design system from the 2026-10 redesign
([handoff](design/redesign-2a/README.md)): Source Serif 4 throughout, cyan for
interaction and selection, magenta reserved for warnings and destructive actions,
and whitespace rather than boxes between sections, in light and dark themes. The
header keeps this raster mark and the name "Alexandria Server". The font (SIL OFL
1.1, `@fontsource/source-serif-4` 5.3.0) and Phosphor duotone icons (MIT,
`@phosphor-icons/core` 2.1.1) are vendored under `internal/alexandria/web/` with
their licences, so pages load nothing from other sites.
