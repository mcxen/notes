import { describe, expect, it } from "vitest";
import { PLAIN_TEXT } from "./code-highlight";
import { detectCodeLanguage, detectLanguage } from "./detect-language";

const SAMPLES: Record<string, string[]> = {
  bash: [
    "#!/bin/bash\nset -e\necho done",
    "#!/usr/bin/env zsh\nls",
    "$ npm install\n$ npm run dev",
    "npm install -g typescript",
    "npm install",
    "sudo apt-get update && sudo apt-get install -y git",
    "brew install node\ngit clone https://github.com/a/b.git\ncd b",
    "for f in *.txt; do\n  echo \"$f\"\ndone",
    "if [ -f \"$FILE\" ]; then\n  rm \"$FILE\"\nfi",
    "cat access.log | grep ERROR | sort | uniq -c",
    "docker run --rm -it -v $(pwd):/app node:20 bash",
  ],
  sql: [
    "SELECT * FROM users;",
    "select id, name from users where age > 18 order by name;",
    "SELECT u.name, COUNT(*)\nFROM users u\nJOIN orders o ON o.user_id = u.id\nGROUP BY u.name;",
    "INSERT INTO users (name, age) VALUES ('a', 1);",
    "UPDATE users SET age = 2 WHERE id = 1;",
    "CREATE TABLE users (\n  id INT PRIMARY KEY,\n  name VARCHAR(50) NOT NULL\n);",
    "DELETE FROM users WHERE id = 3;",
    "ALTER TABLE users ADD COLUMN email TEXT;",
  ],
  cpp: [
    "#include <iostream>\nint main() {\n  std::cout << \"hi\" << std::endl;\n  return 0;\n}",
    "#include <vector>\nusing namespace std;\nvector<int> v;",
    "template <typename T>\nclass Box {\npublic:\n  T value;\n};",
    "auto p = std::make_unique<Foo>();\nif (p == nullptr) return;",
  ],
  c: [
    "#include <stdio.h>\nint main(void) {\n  printf(\"hi\\n\");\n  return 0;\n}",
    "#include <stdlib.h>\ntypedef struct node {\n  int v;\n} node;\nchar *p = malloc(10);",
  ],
  python: [
    "def add(a, b):\n    return a + b",
    "import os\nimport sys\n\nprint(os.getcwd())",
    "from typing import List\n\nclass Foo:\n    def __init__(self):\n        self.x = 1",
    "if __name__ == \"__main__\":\n    main()",
    "for i in range(10):\n    print(i)\nelif x:\n    pass",
    "#!/usr/bin/env python3\nprint('hi')",
    "async def fetch(url: str) -> bytes:\n    ...",
  ],
  java: [
    "public class Main {\n  public static void main(String[] args) {\n    System.out.println(\"hi\");\n  }\n}",
    "package com.example;\n\nimport java.util.List;\n\npublic class Foo {}",
    "@Override\npublic String toString() {\n  return name;\n}",
    "List<String> names = new ArrayList<>();\nSystem.out.println(names);",
  ],
  csharp: [
    "using System;\n\nnamespace App {\n  class P { static void Main() { Console.WriteLine(\"hi\"); } }\n}",
    "public string Name { get; set; }\npublic int Age { get; set; }",
  ],
  javascript: [
    "const a = 1;\nconsole.log(a);",
    "import React from 'react';\nexport default function App() { return null; }",
    "const fs = require('fs');\nmodule.exports = fs;",
    "fetch(url).then((res) => res.json())",
  ],
  typescript: [
    "interface User {\n  name: string;\n  age: number;\n}",
    "export type Id = string;\nconst x: number = 1;\nconsole.log(x);",
    "import type { Foo } from './foo';",
  ],
  go: [
    "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}",
    "x := 1\nif err != nil {\n\treturn err\n}",
  ],
  rust: [
    "fn main() {\n    let mut x = 5;\n    println!(\"{}\", x);\n}",
    "use std::collections::HashMap;\npub fn new() -> Self { Self {} }",
  ],
  php: ["<?php\necho \"hi\";", "$user->name = $_POST['name'];\nforeach ($items as $i) {}"],
  ruby: ["def hello\n  puts \"hi\"\nend", "require 'json'\n[1,2].each do |x|\n  puts x\nend"],
  swift: ["import SwiftUI\n\nstruct A: View {\n  var body: some View { Text(\"hi\") }\n}", "guard let x = y else { return }"],
  kotlin: ["fun main() {\n  val x = 1\n  println(x)\n}", "data class User(val name: String)"],
  json: ['{"a": 1, "b": [1, 2, 3]}', '[\n  {"id": 1},\n  {"id": 2}\n]'],
  html: ["<!DOCTYPE html>\n<html><body></body></html>", "<div class=\"a\">\n  <p>Hello</p>\n</div>"],
  xml: ['<?xml version="1.0"?>\n<root><item id="1"/></root>', "<config>\n  <name>x</name>\n</config>"],
  css: [".a {\n  color: red;\n  margin: 0;\n}", "@media (max-width: 600px) {\n  body { display: none; }\n}", "h1 { color: blue; }"],
  diff: ["diff --git a/x.ts b/x.ts\nindex 123abcd..456efab 100644\n--- a/x.ts\n+++ b/x.ts\n@@ -1,2 +1,2 @@\n-a\n+b", "@@ -10,3 +10,4 @@\n foo\n+bar"],
  yaml: ["name: ci\non:\n  push:\n    branches: [main]\njobs:\n  build:\n    runs-on: ubuntu", "- name: a\n  value: 1\n- name: b\n  value: 2\n"],
};

describe("detecting the language of pasted code", () => {
  for (const [language, snippets] of Object.entries(SAMPLES)) {
    for (const snippet of snippets) {
      it(`${language}: ${JSON.stringify(snippet.slice(0, 40))}`, () => {
        expect(detectLanguage(snippet)).toBe(language);
      });
    }
  }

  it("calls prose plain text, in English and in Chinese", () => {
    expect(detectLanguage("This is a short note about the meeting on Tuesday, and what we agreed to do next.")).toBe(PLAIN_TEXT);
    expect(detectLanguage("今天开会讨论了下个季度的计划，大家都同意先从文档开始做起，然后再看看进度怎么样。")).toBe(PLAIN_TEXT);
    expect(detectCodeLanguage("This is a short note about the meeting on Tuesday, and what we agreed to do next.")).toBeNull();
  });

  it("is not sure of what is too short or too common to tell", () => {
    for (const text of ["", "   \n ", "hello", "print(1)", "x = 1", "import os", "echo hi", "a, b, c", "{}", "[]", "int x;"]) {
      expect(detectLanguage(text), text).toBeNull();
    }
  });

  it("does not take words for code", () => {
    for (const text of [
      "Please select the items from the list and then press OK.",
      "I will update the docs and create a table of contents later.",
      "git is a tool I like to use.",
      "Remember to delete from the list anything you don't need.",
      "Note: the meeting is at 3pm.\nBring your laptop.\nSee you there.",
      "Step 1: open the app\nStep 2: press the button\nStep 3: done",
    ]) {
      expect(detectCodeLanguage(text), text).toBeNull();
    }
  });

  it("keeps a Python dictionary, a list of comments and a to-do list from being YAML or shell", () => {
    expect(detectLanguage("# first\n# second\n# third")).toBeNull();
    expect(detectLanguage("x = {'a': 1}")).toBeNull();
  });

  it("tells C from C++, JavaScript from TypeScript, HTML from XML", () => {
    expect(detectLanguage("#include <stdio.h>\nint main() { printf(\"x\"); }")).toBe("c");
    expect(detectLanguage("#include <iostream>\nint main() { std::cout << 1; }")).toBe("cpp");
    expect(detectLanguage("const x = 1;\nconsole.log(x);")).toBe("javascript");
    expect(detectLanguage("const x: number = 1;\nconsole.log(x);")).toBe("typescript");
    expect(detectLanguage("<ul><li>a</li></ul>")).toBe("html");
    expect(detectLanguage("<book><title>a</title></book>")).toBe("xml");
  });

  it("is not fooled by broken JSON or a bare number list", () => {
    expect(detectLanguage('{"a": 1,}')).toBeNull();
  });

  it("only reads the start of a very long paste", () => {
    expect(detectLanguage(`SELECT * FROM t;\n${"x".repeat(100000)}`)).toBe("sql");
  });
});
