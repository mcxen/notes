// @vitest-environment happy-dom
import { describe, expect, it } from "vitest";
import { orderedBlocks } from "../../core/block-tree";
import { inlinePlainText } from "../../core/inline-content";
import { markdownToFragment } from "./adapters";
import { chooseWebOrMarkdownFragment, textToFragment } from "./block-clipboard";
import { htmlToFragment, MAX_PASTED_IMAGES } from "./html-paste";

let counter = 0;
const id = () => `block_${(counter += 1).toString(16).padStart(8, "0")}`;

describe("pasting Markdown-like text", () => {
  it("extracts Discourse-style and standard Markdown images as ImageSlots", () => {
    counter = 0;
    const sample = [
      "1.我找到了这个网站 https://shifter.io/cn/web-proxy",
      "",
      "进入这个网站，完全不需要注册，就可以用这个网站的网页代理，实现真实的住宅IP访问（这是我们本次注册成功的关键点）",
      "![image|690x278](https://cdn3.ldstatic.com/original/4X/1/a/5/1a54ae68778bae52198b0fce3464aafb628bf8ae.jpeg)",
      "",
      "2.网站框输入muse.ai，节点建议选日本，点浏览",
      "![image|690x265](https://cdn3.ldstatic.com/original/4X/6/6/7/6674bec206856d19f60f3a7102275ef74c295b5d.jpeg)",
    ].join("\n");

    const fragment = textToFragment(sample, id)!;
    expect(fragment).not.toBeNull();
    expect(fragment.images).toHaveLength(2);
    expect(fragment.images[0].src).toBe("https://cdn3.ldstatic.com/original/4X/1/a/5/1a54ae68778bae52198b0fce3464aafb628bf8ae.jpeg");
    expect(fragment.images[0].label).toBe("image");
    expect(fragment.images[1].src).toBe("https://cdn3.ldstatic.com/original/4X/6/6/7/6674bec206856d19f60f3a7102275ef74c295b5d.jpeg");

    const blocks = orderedBlocks(fragment.tree);
    expect(blocks.map((b) => inlinePlainText(b.content))).toEqual([
      "1.我找到了这个网站 https://shifter.io/cn/web-proxy",
      "进入这个网站，完全不需要注册，就可以用这个网站的网页代理，实现真实的住宅IP访问（这是我们本次注册成功的关键点）",
      "image",
      "2.网站框输入muse.ai，节点建议选日本，点浏览",
      "image",
    ]);
    // Bare URL in the first line is still linkified.
    expect(blocks[0].content.find((r) => r.marks?.link)?.marks?.link).toBe("https://shifter.io/cn/web-proxy");
    // Image placeholder holds a link to its source until fetched.
    expect(fragment.tree.by_id[fragment.images[0].blockId].content).toEqual([
      { text: "image", marks: { link: fragment.images[0].src } },
    ]);
  });

  it("handles single-line Markdown images while leaving plain single-line text alone", () => {
    counter = 0;
    expect(textToFragment("just one line", id)).toBeNull();
    expect(textToFragment("https://example.com", id)).toBeNull();

    const singleImage = textToFragment("![shot|690x278](https://example.com/a.jpeg)", id)!;
    expect(singleImage).not.toBeNull();
    expect(singleImage.images).toHaveLength(1);
    expect(singleImage.images[0]).toMatchObject({
      src: "https://example.com/a.jpeg",
      label: "shot",
    });
  });

  it("splits inline images out of text lines and handles linked images", () => {
    counter = 0;
    const fragment = markdownToFragment(
      'Before ![pic](https://example.com/1.png "title") after\n[![badge](https://example.com/2.png)](https://example.com)',
      id,
    );
    expect(fragment.images.map((img) => img.src)).toEqual([
      "https://example.com/1.png",
      "https://example.com/2.png",
    ]);
    expect(orderedBlocks(fragment.tree).map((b) => inlinePlainText(b.content))).toEqual([
      "Before",
      "pic",
      "after",
      "badge",
    ]);
  });

  it("caps queued Markdown images at MAX_PASTED_IMAGES", () => {
    counter = 0;
    const md = Array.from({ length: MAX_PASTED_IMAGES + 5 }, (_, i) => `![n${i}](https://example.com/${i}.png)`).join("\n");
    const fragment = markdownToFragment(md, id);
    expect(fragment.images).toHaveLength(MAX_PASTED_IMAGES);
    expect(Object.keys(fragment.tree.by_id)).toHaveLength(MAX_PASTED_IMAGES + 5);
  });

  it("prefers Markdown over plain HTML with auto-linked URLs, but keeps rich HTML", () => {
    counter = 0;
    const plainText = [
      "1.网站 https://shifter.io/cn/web-proxy",
      "![image|690x278](https://cdn3.ldstatic.com/original/4X/1/a/5/1a54ae68778bae52198b0fce3464aafb628bf8ae.jpeg)",
    ].join("\n");
    const plainHtml = [
      '<p>1.网站 <a href="https://shifter.io/cn/web-proxy">https://shifter.io/cn/web-proxy</a></p>',
      '<p>![image|690x278](<a href="https://cdn3.ldstatic.com/original/4X/1/a/5/1a54ae68778bae52198b0fce3464aafb628bf8ae.jpeg">https://cdn3.ldstatic.com/original/4X/1/a/5/1a54ae68778bae52198b0fce3464aafb628bf8ae.jpeg</a>)</p>',
    ].join("");
    const htmlFrag = htmlToFragment(plainHtml, id);
    const mdFrag = textToFragment(plainText, id);
    const chosen = chooseWebOrMarkdownFragment(htmlFrag, mdFrag)!;
    expect(chosen.images).toHaveLength(1);
    expect(chosen.images[0].src).toBe("https://cdn3.ldstatic.com/original/4X/1/a/5/1a54ae68778bae52198b0fce3464aafb628bf8ae.jpeg");

    const richHtml = htmlToFragment('<h1>Heading</h1><p><strong>Bold</strong> ![not](https://example.com/a.png)</p>', id);
    const chosenRich = chooseWebOrMarkdownFragment(richHtml, mdFrag)!;
    expect(chosenRich).toBe(richHtml);
  });
});
