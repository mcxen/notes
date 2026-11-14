import { BLOCK_ORDER_STEP, createBlock } from "../../core/block-tree";
import { inlinePlainText, isKeptLink, normalizeInlineContent } from "../../core/inline-content";
import { tableFromCells } from "../../core/simple-table";
import type { Block, BlockKind, BlockTree, InlineContent } from "../../core/types";
import { gridFromTable } from "./table-clipboard";
import { inlineFromNodes } from "./rich-text";

/* Content copied from a web page, as blocks. The clipboard holds the page's
   HTML beside its plain words, and the HTML is where the pictures, the
   headings, the lists, the links and the tables are. What is read is untrusted
   — any page on the web may have written it: only the kinds of element Notes
   has a block for are read, scripts, styles and anything that runs are
   dropped, links keep only the addresses Notes keeps (`isKeptLink`), and a
   picture is only an address until the app has fetched it.

   A picture is made a line of its own with its address as a link, which is
   what stays if it cannot be fetched; the paste then replaces that line with
   the picture once its bytes are in hand (`ImageSlot`). */

/** A picture the HTML named, and the line that stands for it until it is fetched. */
export interface ImageSlot {
  /** The block holding the link. */
  blockId: string;
  src: string;
  /** The words of that block as made, to tell it from one the person has since written in. */
  label: string;
}

export interface HtmlFragment {
  tree: BlockTree;
  images: ImageSlot[];
}

/** The most blocks and pictures taken from one paste. */
export const MAX_PASTED_BLOCKS = 3000;
export const MAX_PASTED_IMAGES = 40;

/** Elements that are never read, and the text inside them with them. */
const SKIPPED = new Set([
  "SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "HEAD", "META", "LINK", "TITLE", "SVG", "CANVAS", "IFRAME", "OBJECT", "EMBED",
  "VIDEO", "AUDIO", "BUTTON", "SELECT", "TEXTAREA", "FORM", "NAV", "CLIPBOARD-COPY", "TOOLTIP", "DIALOG",
]);

/** Elements that break the words into lines of their own. */
const BLOCKS = new Set([
  "P", "DIV", "SECTION", "ARTICLE", "MAIN", "HEADER", "FOOTER", "ASIDE", "FIGURE", "FIGCAPTION", "ADDRESS", "DL", "DT", "DD",
  "H1", "H2", "H3", "H4", "H5", "H6", "UL", "OL", "LI", "BLOCKQUOTE", "PRE", "HR", "TABLE", "DETAILS", "SUMMARY", "CENTER",
]);

/** A page's web address for a picture, or "" when it is not one that can be fetched. */
function pictureSource(img: Element): string {
  const raw = (img.getAttribute("src") || img.getAttribute("data-src") || img.getAttribute("data-original") || "").trim();
  if (!raw) return "";
  if (/^data:image\/(?:png|jpe?g|gif|webp);base64,/i.test(raw)) return raw;
  return /^https?:\/\//i.test(raw) && raw.length < 4000 ? raw : "";
}

/** A tracking pixel or a spacer: drawn at a pixel or two. */
function tiny(img: Element): boolean {
  const size = (name: string) => Number.parseInt(img.getAttribute(name) ?? "", 10);
  const width = size("width");
  const height = size("height");
  return (Number.isFinite(width) && width <= 2) || (Number.isFinite(height) && height <= 2);
}

/** Styles that Google Docs, Word and web pages use in place of the elements:
 *  read as the marks they stand for, and the wrappers that say "not bold"
 *  as no mark. */
function normalizeStyles(root: Element): void {
  for (const element of [...root.querySelectorAll<HTMLElement>("b, strong, span, i, em")]) {
    const style = element.style;
    const bold = /^(bold|[6-9]00)$/.test(style.fontWeight);
    const plain = /^(normal|[1-5]00)$/.test(style.fontWeight);
    if (plain && (element.tagName === "B" || element.tagName === "STRONG")) {
      element.replaceWith(...element.childNodes);
      continue;
    }
    if (element.tagName !== "SPAN") continue;
    let inner: Node[] = [...element.childNodes];
    const wrap = (tag: string) => {
      const mark = element.ownerDocument.createElement(tag);
      mark.append(...inner);
      inner = [mark];
    };
    if (bold) wrap("strong");
    if (style.fontStyle === "italic") wrap("em");
    if (style.textDecorationLine.includes("underline") || style.textDecoration.includes("underline")) wrap("u");
    if (style.textDecorationLine.includes("line-through") || style.textDecoration.includes("line-through")) wrap("s");
    if (inner.length !== element.childNodes.length || inner[0] !== element.firstChild) element.replaceChildren(...inner);
  }
}

/** The language a code block's markup names: GitHub's `highlight-source-js`,
 *  Prism's and highlight.js's `language-js`. */
function codeLanguage(pre: Element): string | undefined {
  for (let element: Element | null = pre.querySelector("code") ?? pre; element; element = element.parentElement) {
    const match = /(?:language|lang|highlight-source)-([\w+#-]+)/i.exec(element.getAttribute("class") ?? "");
    if (match) return match[1].toLowerCase();
    if (element.tagName === "ARTICLE" || element.tagName === "BODY") break;
  }
  return undefined;
}

class Reader {
  readonly byId: Record<string, Block> = {};
  readonly images: ImageSlot[] = [];
  private readonly counts = new Map<string, number>();

  constructor(private readonly newId: () => string) {}

  get full(): boolean {
    return Object.keys(this.byId).length >= MAX_PASTED_BLOCKS;
  }

  add(parent: string | null, kind: BlockKind, fields: Partial<Omit<Block, "id" | "kind">> = {}): Block | null {
    if (this.full) return null;
    const key = parent ?? "root";
    const order = ((this.counts.get(key) ?? 0) + 1) * BLOCK_ORDER_STEP;
    this.counts.set(key, order / BLOCK_ORDER_STEP);
    const block = createBlock(this.newId(), kind, { ...fields, parent_id: parent, order });
    this.byId[block.id] = block;
    return block;
  }

  /** The nodes under `container`, in lines: its words between block elements
   *  are text lines, its block elements their own. */
  children(container: Node, parent: string | null): void {
    let buffer: Node[] = [];
    const flush = () => {
      this.text(buffer, parent);
      buffer = [];
    };
    for (const node of container.childNodes) {
      if (node.nodeType === Node.TEXT_NODE) {
        buffer.push(node);
        continue;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) continue;
      const element = node as Element;
      if (SKIPPED.has(element.tagName)) continue;
      if (element.tagName === "IMG") {
        flush();
        this.picture(element, parent);
        continue;
      }
      if (element.tagName === "BR") {
        buffer.push(element);
        continue;
      }
      if (!BLOCKS.has(element.tagName)) {
        // Inline markup is words; a picture among them is a line of its own.
        if (element.querySelector("img, table, pre, ul, ol, blockquote, h1, h2, h3, h4, h5, h6, hr, p, div")) {
          flush();
          this.children(element, parent);
          continue;
        }
        buffer.push(element);
        continue;
      }
      flush();
      this.block(element, parent);
    }
    flush();
  }

  private text(nodes: Node[], parent: string | null): void {
    const content = this.words(nodes);
    if (content) this.add(parent, "text", { content });
  }

  /** The marks and words of some nodes, trimmed; null when there are none. */
  private words(nodes: Node[]): InlineContent | null {
    if (!nodes.length) return null;
    const holder = (nodes[0].ownerDocument ?? document).createElement("span");
    for (const node of nodes) holder.append(node.cloneNode(true));
    const content = inlineFromNodes(holder, { collapse: true });
    const text = inlinePlainText(content);
    if (!text.trim()) return null;
    const lead = text.length - text.trimStart().length;
    const end = text.trimEnd().length;
    return trim(content, lead, end);
  }

  private picture(img: Element, parent: string | null): void {
    const src = pictureSource(img);
    const alt = (img.getAttribute("alt") ?? "").trim();
    if (!src) {
      // One that cannot be had keeps its words, if it had any.
      if (alt) this.add(parent, "text", { content: [{ text: alt }] });
      return;
    }
    if (tiny(img)) return;
    const named = alt || (src.startsWith("data:") ? "image" : src);
    // What stays if the picture cannot be fetched: its words as a link to it.
    const content: InlineContent = src.startsWith("data:") || !isKeptLink(src) ? [{ text: named }] : [{ text: named, marks: { link: src } }];
    const block = this.add(parent, "text", { content });
    // Past the limit a picture stays a link, as one that could not be fetched.
    if (block && this.images.length < MAX_PASTED_IMAGES) this.images.push({ blockId: block.id, src, label: inlinePlainText(content) });
  }

  private block(element: Element, parent: string | null): void {
    const tag = element.tagName;
    const heading = /^H([1-6])$/.exec(tag);
    if (heading) {
      const content = this.words([...element.childNodes].filter((node) => !(node instanceof Element && SKIPPED.has(node.tagName))));
      if (content) this.add(parent, (`heading${Math.min(3, Number(heading[1]))}`) as BlockKind, { content });
      return;
    }
    switch (tag) {
      case "HR":
        this.add(parent, "divider");
        return;
      case "PRE": {
        const text = (element.textContent ?? "").replace(/\r\n?/g, "\n").replace(/\n$/, "");
        if (text.trim()) this.add(parent, "code", { content: [{ text }], language: codeLanguage(element) });
        return;
      }
      case "TABLE": {
        const grid = gridFromTable(element);
        if (grid) {
          this.add(parent, "table", { table: tableFromCells(grid.rows, (kind) => this.newId().replace(/^block/, kind === "column" ? "col" : "row"), { headerRow: grid.header }) });
        }
        return;
      }
      case "UL":
      case "OL":
        for (const item of element.children) if (item.tagName === "LI") this.item(item, parent, tag === "OL");
        return;
      case "LI":
        this.item(element, parent, false);
        return;
      case "BLOCKQUOTE": {
        const quote = this.add(parent, "quote", { content: [] });
        if (!quote) return;
        const before = Object.keys(this.byId).length;
        this.children(element, quote.id);
        this.liftFirstLine(quote);
        if (Object.keys(this.byId).length === before) delete this.byId[quote.id];
        return;
      }
      case "DETAILS": {
        const summary = [...element.children].find((child) => child.tagName === "SUMMARY");
        const toggle = this.add(parent, "toggle", { content: this.words(summary ? [...summary.childNodes] : []) ?? [{ text: "…" }] });
        if (!toggle) return;
        const rest = element.cloneNode(true) as Element;
        rest.querySelector(":scope > summary")?.remove();
        this.children(rest, toggle.id);
        return;
      }
      default:
        // A container: its lines are the page's lines.
        this.children(element, parent);
    }
  }

  /** A list item: its words, then what is nested in it. */
  private item(li: Element, parent: string | null, numbered: boolean): void {
    const checkbox = li.querySelector(":scope > input[type=checkbox], :scope > p > input[type=checkbox]");
    const kind: BlockKind = checkbox ? "todo" : numbered ? "numbered" : "bullet";
    const item = this.add(parent, kind, { content: [], ...(checkbox ? { checked: checkbox.hasAttribute("checked") || (checkbox as HTMLInputElement).checked } : {}) });
    if (!item) return;
    this.children(li, item.id);
    this.liftFirstLine(item);
  }

  /** An item's or a quote's first line of words is its own words: the text
   *  line made for it moves up into it. */
  private liftFirstLine(owner: Block): void {
    const first = Object.values(this.byId)
      .filter((block) => block.parent_id === owner.id)
      .toSorted((a, b) => a.order - b.order)[0];
    if (first?.kind !== "text" || this.images.some((image) => image.blockId === first.id)) return;
    this.byId[owner.id] = { ...owner, content: first.content };
    delete this.byId[first.id];
  }
}

/** The runs of `content` between two offsets of its words. */
function trim(content: InlineContent, start: number, end: number): InlineContent {
  const out: InlineContent = [];
  let at = 0;
  for (const run of content) {
    const length = run.text.length;
    const from = Math.max(start - at, 0);
    const to = Math.min(end - at, length);
    if (to > from) out.push({ ...run, text: run.text.slice(from, to) });
    at += length;
  }
  return normalizeInlineContent(out);
}

/** What some copied HTML holds, as blocks and the pictures it names — or null
 *  when it holds nothing that plain text would not: a line or two of unmarked
 *  words, which the editor pastes among the words like anything typed. */
export function htmlToFragment(html: string, newId: () => string): HtmlFragment | null {
  if (!html || typeof DOMParser === "undefined") return null;
  const document = new DOMParser().parseFromString(html, "text/html");
  normalizeStyles(document.body);
  const reader = new Reader(newId);
  reader.children(document.body, null);
  const blocks = Object.values(reader.byId);
  if (!blocks.length) return null;
  const marked = (block: Block) => block.content.some((run) => run.marks && Object.keys(run.marks).length > 0);
  const rich = reader.images.length > 0
    || blocks.length > 1
    || blocks.some((block) => block.kind !== "text" || marked(block));
  return rich ? { tree: { by_id: reader.byId }, images: reader.images } : null;
}
