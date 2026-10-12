"""Export the architecture's Mermaid blocks as portable SVG and PNG diagrams.

Requires Mermaid CLI (mmdc) and librsvg (rsvg-convert) on PATH.
"""

import argparse
from pathlib import Path
import re
import subprocess
import tempfile
import xml.etree.ElementTree as ET


DIRECTORY = Path(__file__).resolve().parent
SVG = "http://www.w3.org/2000/svg"
NS = {"s": SVG}
PNG_MIN_SIDE = 1024
DOCUMENTS = {
    "architecture.md": ("source-repositories", "onyx-sync"),
    "architecture-aws.md": ("aws-source-repositories", "aws-onyx-sync"),
}
ET.register_namespace("", SVG)


def portable_svg(path):
    root = ET.parse(path).getroot()
    if root.findall(".//s:foreignObject", NS):
        raise ValueError("HTML labels are not portable; set top-level htmlLabels=false")
    # Mermaid splits each line into per-word spans. Some desktop renderers lose
    # spaces or overlap words. Keep one span per line, preserving the uniform
    # bold weight used by the software headers in the canonical diagrams.
    for row in root.iter(f"{{{SVG}}}tspan"):
        if "text-outer-tspan" in row.get("class", "").split():
            text = "".join(row.itertext())
            weights = {child.get("font-weight", "normal") for child in row
                       if "".join(child.itertext()).strip()}
            if weights == {"bold"}:
                row.set("font-weight", "bold")
            for child in list(row):
                row.remove(child)
            row.text = text
    for node in root.findall(".//s:g", NS):
        if "node" in node.get("class", "").split():
            label = "".join("".join(t.itertext()) for t in node.findall(".//s:text", NS))
            if not label.strip():
                raise ValueError(f"Missing native SVG label: {node.get('id')}")
    edges = [p for p in root.findall(".//s:path", NS)
             if "flowchart-link" in p.get("class", "").split()]
    labels = [g for g in root.findall(".//s:g", NS)
              if "edgeLabel" in g.get("class", "").split()]
    if len(labels) != len(edges):
        raise ValueError("Every arrow must have a label")
    for label in labels:
        if not any("".join(t.itertext()).strip() for t in label.findall(".//s:text", NS)):
            raise ValueError("Empty arrow label")
        for background in label.findall(".//s:rect", NS):
            # Opaque backing keeps the arrow from striking through its label.
            background.set("style", "fill:white;fill-opacity:1;opacity:1;stroke:none")
    for element in root.iter():
        dash = re.search(r"stroke-dasharray:([^;!]+)", element.get("style", ""))
        if dash:
            # A presentation attribute also works in renderers which reject
            # Mermaid's space-separated dash list in inline CSS.
            element.set("stroke-dasharray", dash.group(1).strip().replace(" ", ","))
    root.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
    _, _, width, height = root.get("viewBox").split()
    root.set("width", width)
    root.set("height", height)
    # CSS background-color alone is ignored by some SVG image viewers.
    root.insert(0, ET.Element(f"{{{SVG}}}rect", {
        "width": width, "height": height, "fill": "white", "stroke": "none",
    }))
    ET.ElementTree(root).write(path, encoding="unicode")


def export_png(svg_path):
    root = ET.parse(svg_path).getroot()
    width, height = map(float, root.get("viewBox").split()[2:])
    zoom = max(1.0, PNG_MIN_SIDE / min(width, height))
    subprocess.run(["rsvg-convert", str(svg_path), "--zoom", str(zoom),
                    "-o", str(svg_path.with_suffix(".png"))], check=True)


def export_document(document, names, puppeteer_config):
    blocks = re.findall(r"```mermaid\n(.*?)\n```", (DIRECTORY.parent / document).read_text(), re.S)
    if len(blocks) != len(names):
        raise ValueError("Update the export mapping when adding or removing diagrams")
    with tempfile.TemporaryDirectory(prefix="alexandria-diagrams-") as temporary:
        for name, block in zip(names, blocks):
            source = Path(temporary) / f"{name}.mmd"
            source.write_text(block + "\n")
            output = DIRECTORY / f"{name}.svg"
            command = ["mmdc", "-i", str(source), "-o", str(output),
                       "-c", str(DIRECTORY / "mermaid.json"), "-b", "white"]
            if puppeteer_config:
                command.extend(["-p", str(puppeteer_config.resolve())])
            subprocess.run(command, check=True)
            portable_svg(output)
            export_png(output)
            print(f"Exported {name}: SVG text labels and PNG preview", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--puppeteer-config", type=Path)
    parser.add_argument("--document", choices=DOCUMENTS,
                        help="Export one architecture document (default: all)")
    args = parser.parse_args()
    for document, names in DOCUMENTS.items():
        if args.document is None or document == args.document:
            export_document(document, names, args.puppeteer_config)


if __name__ == "__main__":
    main()
