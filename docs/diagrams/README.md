# Architecture diagram exports

The canonical editable diagrams are the Mermaid blocks in the
[general architecture](../architecture.md) and its separate
[AWS deployment variant](../architecture-aws.md). Each has a generated SVG and PNG:

- `source-repositories.svg`: implemented and planned source interfaces and logical data stores.
- `onyx-sync.svg`: selected native BOOX data paths, with application and private
  backing services grouped inside the overall Alexandria Server deployment.
- `aws-source-repositories.svg`: the corresponding logical source view mapped to AWS.
- `aws-onyx-sync.svg`: the AWS target deployment, with VPC, application tasks,
  private backing services and S3 placement. Deployment gaps are documented in
  the AWS guide as **To be implemented**.

Generated with Mermaid CLI `11.12.0`, neutral theme, Arial 16px, white background
and the checked-in [configuration](mermaid.json). No external image assets or
live data are needed. Keep the Markdown and exports together when revising.
Source colors are defined in the canonical Mermaid blocks: amber for BOOX, blue
for Alexandria Client, purple for reMarkable, teal for Supernote, and slate gray
for shared services. Planned components retain dashed borders and explicit labels.
Every arrow names the exchanged data or operation. The Onyx detail uses Mermaid's
ELK layout to route connections around intermediate boxes.
Node labels use Mermaid Markdown strings: a bold software header followed by
plain role and responsibility lines. Planned implementations and unselected
storage are labeled explicitly. Keep each header line entirely bold so its
weight survives portable SVG normalization.

With Mermaid CLI and librsvg (`rsvg-convert`) installed, run from the repository root:

```sh
python3 docs/diagrams/export.py
```

Use `--document architecture.md` or `--document architecture-aws.md` to export
only one variant. The default regenerates both.

To use an existing Chrome installation, pass `--puppeteer-config /path/to/config.json`.
For example, that file can contain:

```json
{"executablePath":"/usr/bin/google-chrome"}
```

**Viewer compatibility:** Mermaid needs top-level `htmlLabels=false`, not only
`flowchart.htmlLabels=false`. Otherwise it can embed HTML labels in SVG
`foreignObject` elements, which desktop viewers may omit. The exporter rejects
those elements, checks that every node has a native SVG text label, and combines
per-word spans into single lines to preserve spacing in desktop renderers while
retaining bold software headers. Text
remains editable in SVG editors. An explicit white background also works outside
web browsers. PNG previews are rendered directly from the final SVG using librsvg,
with the shorter side at least 1024 pixels. Scaling preserves the aspect ratio
and never reduces diagrams that already exceed this minimum.
The exporter also rejects unlabeled arrows and gives arrow labels opaque
backgrounds so connection lines do not obscure the text.
The SVG exports have also been rendered through Inkscape and visually checked for
readable labels.
