import { describe, expect, it } from "vitest";
import { orderedBlocks } from "../../core/block-tree";
import { markdownToBlockTree } from "./adapters";

let counter = 0;
const id = () => `block_${(counter += 1).toString(16).padStart(8, "0")}`;

const languages = (markdown: string) => orderedBlocks(markdownToBlockTree(markdown, id), null)
  .filter((block) => block.kind === "code")
  .map((block) => block.language);

describe("a Markdown fence's language", () => {
  it("is the one it names, or the one its code is written in when it names none", () => {
    expect(languages("```python\nSELECT * FROM t;\n```")).toEqual(["python"]);
    expect(languages("```\nSELECT * FROM users WHERE id = 1;\n```")).toEqual(["sql"]);
    expect(languages("```\n$ git status\n$ git diff\n```")).toEqual(["bash"]);
    expect(languages("```\njust some words\n```")).toEqual([undefined]);
  });
});
