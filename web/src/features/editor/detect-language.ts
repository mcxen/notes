import { PLAIN_TEXT } from "./code-highlight";

/* Which language a piece of pasted code is written in, guessed from the code
   alone: a shebang, `#include`, `def`, `SELECT … FROM`, a `$ ` prompt…

   Every language has a few patterns, each worth some points; the language with
   the most points wins — if it has enough of them and clearly more than the
   best language that is not its close cousin (C and C++, JavaScript and
   TypeScript, HTML and XML are told apart on their own). When it is not clear,
   the answer is null: a block left as plain text is better than one coloured
   as the wrong language. */

/** A pattern, the points one match is worth, and how many matches count. */
type Rule = readonly [pattern: RegExp, points: number, cap?: number];

const MIN_SCORE = 4;
/** Only the start of a long paste is read: its language shows by then. */
const MAX_READ = 8000;

/* Case-sensitive unless said otherwise; `^` is the start of a line. */
const RULES: Record<string, readonly Rule[]> = {
  bash: [
    [/^\s*\$ \S/gm, 3, 3],
    [/^\s*(?:sudo\s+)?(?:apt(?:-get)?|brew|npm|npx|yarn|pnpm|pip3?|docker|git|curl|wget|chmod|chown|mkdir|kubectl|systemctl|ssh|scp|export|sudo)\s+(?!(?:is|was|are|the|a|an|and|or|to|for|of|in|on|it|you|we)\b)\S/gm, 4, 2],
    [/^\s*(?:cd|ls|grep|cat|echo|rm|cp|mv|tar|make|node|cargo|touch|kill|sed|awk|find)\s+\S/gm, 2, 3],
    [/^\s*(?:fi|done|esac)\s*$/gm, 3, 2],
    [/^\s*if\s+\[\[?\s.*\]\]?\s*;?\s*(?:then)?\s*$/gm, 4],
    [/^\s*for\s+\w+\s+in\b.*(?:;\s*do|$)/gm, 3],
    [/\bthen\s*$/gm, 2],
    [/\$\([^)\n]*\)/g, 2, 2],
    [/\$\{\w+[^}\n]*\}/g, 1, 2],
    [/\|\s*(?:grep|awk|sed|sort|uniq|xargs|wc|head|tail|tee|cut|tr)\b/g, 2, 2],
    [/\s&&\s/g, 1, 2],
    [/\s(?:2>&1|>\s*\/dev\/null)/g, 2],
    [/^[A-Z_]{2,}=\S/gm, 2, 2],
  ],
  python: [
    [/^\s*(?:async\s+)?def\s+\w+\s*\(.*\)\s*(?:->\s*[^:]+)?:\s*(?:#.*)?$/gm, 4, 2],
    [/^\s*class\s+\w+\s*(?:\(.*\))?:\s*$/gm, 4, 2],
    [/^\s*from\s+[\w.]+\s+import\s+\S/gm, 4, 2],
    [/^\s*import\s+[a-z_][\w.]*(?:\s+as\s+\w+)?(?:\s*,\s*[a-z_][\w.]*)*\s*$/gm, 2, 3],
    [/\bif\s+__name__\s*==\s*['"]__main__['"]/g, 6],
    [/^\s*elif\b.*:\s*(?:#.*)?$/gm, 4],
    [/^\s*(?:if|while|for|with|try|else|except|finally)\b[^{};]*:\s*(?:#.*)?$/gm, 2, 3],
    [/\bself\.\w+/g, 2, 2],
    [/\bprint\(/g, 1],
    [/\b(?:None|True|False)\b/g, 1],
    [/\blambda\b[^:\n]*:/g, 2],
    [/\bf["'][^"'\n]*\{[^}\n]+\}/g, 2],
    [/\bin\s+range\(/g, 2],
    [/"""|'''/g, 1],
  ],
  java: [
    [/^\s*package\s+[\w.]+;/gm, 3],
    [/^\s*import\s+(?:static\s+)?(?:java|javax|org|com|android)\.[\w.*]+;/gm, 5, 2],
    [/\b(?:public|private|protected)\s+(?:static\s+)?(?:final\s+)?(?:abstract\s+)?class\s+\w+/g, 4],
    [/\bpublic\s+static\s+void\s+main\s*\(/g, 8],
    [/\bSystem\.(?:out|err)\.print(?:ln|f)?\(/g, 5],
    [/\b(?:public|private|protected)\s+(?:static\s+)?(?:final\s+)?(?!function\b|class\b)[\w<>\[\],?]+\s+\w+\s*\([^)]*\)\s*(?:throws\s+[\w, ]+)?\s*\{?\s*$/gm, 2, 3],
    [/^\s*@(?:Override|Autowired|Test|Entity|Service|RestController|Component|Bean|Deprecated|FunctionalInterface)\b/gm, 3, 2],
    [/\bthrows\s+[A-Z]\w*/g, 2],
    [/\b(?:extends|implements)\s+[A-Z]\w*/g, 2],
    [/\bnew\s+[A-Z]\w*(?:<[^>]*>)?\s*\(/g, 1, 2],
    [/\b(?:ArrayList|HashMap|HashSet|List|Map|Set)<[\w, <>?]*>/g, 2, 2],
    [/\bString\[\]/g, 2],
    [/\bString\s+\w+\s*=/g, 1],
  ],
  csharp: [
    [/^\s*using\s+System(?:\.[\w.]+)?;/gm, 6],
    [/^\s*using\s+(?!namespace\b)[A-Z][\w.]*;/gm, 3],
    [/^\s*namespace\s+[A-Z]\w*(?:\.\w+)*\s*(?:\{|;|$)/gm, 3],
    [/\bConsole\.(?:Write|WriteLine|ReadLine)\(/g, 6],
    [/\{\s*get;\s*(?:set;|private set;|init;)?\s*\}/g, 5],
    [/(?<![:\w])string\s+\w+\s*[=;,)]/g, 2, 2],
    [/\bvar\s+\w+\s*=\s*new\b/g, 2],
    [/\basync\s+Task\b|\bTask<\w+>/g, 3],
    [/\bforeach\s*\(\s*\w+\s+\w+\s+in\b/g, 4],
    [/^\s*\[(?:HttpGet|HttpPost|Fact|Test|TestMethod|Serializable|Required|ApiController)\b[^\]]*\]/gm, 3],
    [/\b(?:public|private|protected|internal)\s+(?:static\s+)?(?:class|void|string|int|bool|async|override)\b/g, 2, 2],
    [/\b(?:public|private|protected)\s+(?:static\s+)?(?:final\s+)?class\s+\w+/g, 3],
  ],
  kotlin: [
    [/^\s*(?:override\s+|private\s+|public\s+|internal\s+|suspend\s+)*fun\s+(?:<[^>]*>\s*)?[\w.]+\s*\(/gm, 4, 2],
    [/\bval\s+\w+/g, 3, 2],
    [/\bdata\s+class\b/g, 4],
    [/\bcompanion\s+object\b/g, 4],
    [/\bwhen\s*(?:\(.*\))?\s*\{/g, 3],
    [/^\s*import\s+(?:kotlin|kotlinx|android|androidx|java)\.[\w.*]+\s*$/gm, 4],
    [/^\s*package\s+\w+(?:\.\w+)+\s*$/gm, 2],
    [/\)\s*:\s*[A-Z]\w*\??\s*(?:=|\{)/g, 1],
    [/\?:\s/g, 1],
  ],
  swift: [
    [/^\s*import\s+(?:Foundation|SwiftUI|UIKit|Combine|AppKit|XCTest)\b/gm, 6],
    [/\bfunc\s+\w+\s*(?:<[^>]*>)?\s*\([^)]*\w+\s*:\s*\w+/g, 3, 2],
    [/\b(?:guard\s+(?:let|var)|if\s+let)\b/g, 4, 2],
    [/\bstruct\s+\w+\s*:\s*(?:View|Codable|Hashable|Identifiable|App)\b/g, 5],
    [/\bvar\s+body\s*:\s*some\b/g, 6],
    [/^\s*@(?:State|Binding|Published|ObservedObject|StateObject|main|objc|Environment)\b/gm, 3, 2],
    [/\bnil\b/g, 1],
    [/\blet\s+\w+\s*:\s*[A-Z]\w*/g, 2],
    [/\bfunc\s+\w+\s*\([^)]*\)\s*->\s*\w+/g, 1],
  ],
  go: [
    [/^package\s+main\s*$/gm, 6],
    [/^package\s+\w+\s*$/gm, 3],
    [/\bfunc\s+main\s*\(\s*\)/g, 4],
    [/\bfmt\.(?:Print|Printf|Println|Sprintf|Errorf|Fprintf)\(/g, 6],
    [/:=/g, 2, 2],
    [/^\s*import\s*\(\s*$/gm, 4],
    [/^\s*import\s+"[\w/.-]+"\s*$/gm, 4],
    [/\bfunc\s+(?:\([^)]*\)\s*)?\w+\s*\([^):]*\)/g, 3, 2],
    [/\bgo\s+func\s*\(/g, 3],
    [/\bdefer\s+\w/g, 3],
    [/\bif\s+err\s*!=\s*nil\b/g, 6],
    [/^\s*type\s+\w+\s+(?:struct|interface)\s*\{/gm, 5],
    [/\[\]\w+\{/g, 2],
  ],
  rust: [
    [/\bfn\s+main\s*\(\s*\)/g, 5],
    [/\bfn\s+\w+\s*(?:<[^>]*>)?\s*\(/g, 3, 2],
    [/\blet\s+mut\b/g, 5],
    [/\b(?:println|print|eprintln|format|vec|panic|assert_eq|assert|write|writeln)!\s*[(\[]/g, 4, 2],
    [/^\s*use\s+(?:std|crate|super|self)::/gm, 5],
    [/\bimpl(?:\s*<[^>]*>)?\s+\w+/g, 3],
    [/\bpub\s+(?:fn|struct|enum|mod|use|trait)\b/g, 4, 2],
    [/->\s*(?:Result|Option|i32|u32|i64|u64|usize|String|bool|f64|Self|Vec)\b/g, 2],
    [/&mut\s+\w/g, 2],
    [/#\[derive\(/g, 5],
    [/\.unwrap\(\)/g, 3],
    [/\b(?:Some|Ok|Err)\(/g, 1],
  ],
  php: [
    [/<\?php/g, 10],
    [/\$\w+->\w+/g, 3, 2],
    [/\$_(?:GET|POST|SERVER|SESSION|REQUEST|COOKIE)\b/g, 5],
    [/\b(?:public|private|protected)\s+(?:static\s+)?function\b/g, 4, 2],
    [/\bfunction\s+\w+\s*\([^)]*\$\w+/g, 3],
    [/\becho\s+["'$]/g, 2],
    [/\bforeach\s*\(\s*\$\w+\s+as\b/g, 5],
    [/\$this->/g, 4],
    [/^\s*namespace\s+[\w\\]+;/gm, 3],
    [/^\s*use\s+[\w\\]+\\[\w\\]+;/gm, 3],
    [/^\s*\$\w+\s*=\s/gm, 1, 2],
    [/\?>/g, 2],
  ],
  ruby: [
    [/^\s*def\s+\w+[?!]?(?:\s*\(.*\))?\s*$/gm, 3, 2],
    [/^\s*end\s*$/gm, 3, 2],
    [/\bputs\s+\S/g, 3],
    [/^\s*require(?:_relative)?\s+['"]/gm, 4],
    [/\bdo\s*\|[^|\n]*\|\s*$/gm, 4],
    [/^\s*(?:class|module)\s+[A-Z]\w*(?:\s*<\s*[\w:]+)?\s*$/gm, 4, 2],
    [/\battr_(?:accessor|reader|writer)\b/g, 5],
    [/\{\s*\|\w+(?:,\s*\w+)*\|/g, 3],
    [/@\w+\s*=/g, 2],
    [/:\w+\s*=>/g, 2],
    [/\b(?:unless|elsif)\b/g, 3],
    [/\.each\s+do\b/g, 3],
  ],
  c: [
    [/^\s*#include\s*[<"][\w/.+-]+[>"]/gm, 5],
    [/^\s*#(?:define|ifdef|ifndef|endif|pragma\s+once)\b/gm, 2, 2],
    [/\bint\s+main\s*\(/g, 3],
    [/\b(?:printf|scanf|fprintf|sprintf)\s*\(/g, 2],
    [/\b(?:malloc|free|sizeof|calloc|strcpy|strlen|memcpy)\s*\(/g, 2, 2],
    [/\b(?:void|int|char|float|double)\s+\*?\w+\s*\([^)]*\)\s*\{/g, 2],
    // What only C has: its own headers, and structs without a class around them.
    [/^\s*#include\s*<\w+\.h>/gm, 2],
    [/\bFILE\s*\*/g, 3],
    [/\btypedef\s+struct\b/g, 4],
    [/\bchar\s*\*\s*\w+/g, 2],
    [/\bprintf\s*\(/g, 1],
  ],
  cpp: [
    [/^\s*#include\s*[<"][\w/.+-]+[>"]/gm, 5],
    [/^\s*#(?:define|ifdef|ifndef|endif|pragma\s+once)\b/gm, 2, 2],
    [/\bint\s+main\s*\(/g, 3],
    [/\b(?:printf|scanf|fprintf|sprintf)\s*\(/g, 2],
    [/\b(?:malloc|free|sizeof|calloc|strcpy|strlen|memcpy)\s*\(/g, 2, 2],
    [/\b(?:void|int|char|float|double)\s+\*?\w+\s*\([^)]*\)\s*\{/g, 2],
    // What only C++ has.
    [/^\s*#include\s*<(?:iostream|vector|string|map|set|algorithm|memory|cstdio|cstdlib|cmath|cstring|utility|queue|stack|unordered_map|unordered_set|sstream|fstream|iomanip|functional|thread|mutex|bitset|array|list|deque|numeric|chrono|random|cassert|climits)>/gm, 5],
    [/\bstd::\w+/g, 4, 2],
    [/\busing\s+namespace\s+std\b/g, 5],
    [/\b(?:cout|cin|cerr|endl)\b/g, 3, 2],
    [/\btemplate\s*<[^>]*>/g, 5],
    [/\bclass\s+\w+(?:\s*:\s*(?:public|private|protected)\s+\w+)?\s*\{/g, 3],
    [/^\s*(?:public|private|protected)\s*:\s*$/gm, 4],
    [/\bnullptr\b/g, 4],
    [/\bauto\s+\w+\s*=/g, 3],
    [/\b(?:std::)?(?:vector|map|set|unordered_map|unique_ptr|shared_ptr|pair|array|list|queue|stack)\s*</g, 3],
    [/\b(?:constexpr|noexcept|typename)\b|\bvirtual\s+/g, 3],
    [/\bnamespace\s+\w+\s*\{/g, 3],
    [/\b\w+::\w+\s*\(/g, 1],
  ],
  sql: [
    [/\bSELECT\b[\s\S]{0,400}?\bFROM\b/g, 6],
    [/\bselect\s+(?:\*|[\w.]+\s*,|count\s*\(|distinct\b)[\s\S]{0,400}?\bfrom\s+\w/gi, 5],
    // Statements: in capitals on their own, in any case with the shape of one.
    [/\bINSERT\s+INTO\s+\S+/g, 5],
    [/\binsert\s+into\s+\S+\s*(?:\(|values\b|select\b)/gi, 5],
    [/\bUPDATE\s+\S+\s+SET\b/g, 5],
    [/\bupdate\s+\S+\s+set\s+\w+\s*=/gi, 5],
    [/\bDELETE\s+FROM\s+\S+/g, 5],
    [/\bdelete\s+from\s+\w+\s*(?:where\b|;|$)/gim, 5],
    [/\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMP(?:ORARY)?\s+)?(?:TABLE|INDEX|UNIQUE\s+INDEX|VIEW|DATABASE|SCHEMA|FUNCTION|PROCEDURE|TRIGGER)\b/g, 5],
    [/\bcreate\s+(?:temp(?:orary)?\s+)?table\s+(?:if\s+not\s+exists\s+)?\w+\s*\(/gi, 5],
    [/\bALTER\s+TABLE\b|\bDROP\s+(?:TABLE|INDEX|VIEW|DATABASE)\b/g, 5],
    [/\balter\s+table\s+\w+\s+(?:add|drop|alter|rename)\b|\bdrop\s+table\s+(?:if\s+exists\s+)?\w+\s*;/gi, 5],
    [/\bWHERE\s+[\w.]+\s*(?:=|<|>|!=|<>|like\b|in\b|is\b|between\b)/gi, 2],
    [/\bJOIN\s+\w+[^\n]*\bON\b/gi, 3],
    [/\bGROUP\s+BY\b|\bORDER\s+BY\b|\bHAVING\b/gi, 2, 2],
    [/\bPRIMARY\s+KEY\b|\bNOT\s+NULL\b|\bFOREIGN\s+KEY\b|\bVARCHAR\s*\(/gi, 2, 2],
    [/\bVALUES\s*\(/gi, 2],
    [/\bLIMIT\s+\d+/gi, 1],
  ],
  css: [
    [/^\s*(?:color|background(?:-[\w-]+)?|margin(?:-\w+)?|padding(?:-\w+)?|font(?:-[\w-]+)?|display|position|width|height|min-\w+|max-\w+|border(?:-[\w-]+)?|flex(?:-[\w-]+)?|grid(?:-[\w-]+)?|text-[\w-]+|line-height|overflow(?:-\w)?|top|left|right|bottom|z-index|opacity|cursor|transition|transform|align-[\w-]+|justify-[\w-]+|gap|box-shadow|content|animation(?:-[\w-]+)?)\s*:\s*[^;{}\n]+;?\s*$/gm, 2, 3],
    [/@(?:media|import|keyframes|font-face|charset)\b/g, 4],
    [/^\s*[.#:]?[\w-]+(?:[ >+~,.#:\[][^{};\n]*)?\{\s*$/gm, 1],
    [/\{\s*(?:color|background|margin|padding|display|font|border|width|height)[\w-]*\s*:\s*[^;{}]+;?[^{}]*\}/gi, 5],
  ],
  diff: [
    [/^diff --git /gm, 8],
    [/^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@/gm, 8],
    [/^(?:---|\+\+\+) (?:a\/|b\/|\/dev\/null)\S*/gm, 3, 2],
    [/^index [0-9a-f]{7,}\.\.[0-9a-f]{7,}/gm, 5],
  ],
  // What JavaScript and TypeScript share: each is told from the other below.
  javascript: [
    [/\bconsole\.(?:log|error|warn|info)\(/g, 4],
    [/^\s*(?:export\s+)?(?:const|let|var)\s+(?:\w+|\{[^}]*\}|\[[^\]]*\])\s*(?::\s*[^=\n]+)?=/gm, 2, 3],
    [/\bfunction\s+\w+\s*\([^$)]*\)\s*\{/g, 3, 2],
    [/\)\s*=>\s*[\w{(\['"]/g, 2, 2],
    [/^\s*import\s+.*\s+from\s+['"]/gm, 5],
    [/^\s*import\s+['"]/gm, 2],
    [/^\s*export\s+(?:default|const|function|class|async|interface|type)\b/gm, 4],
    [/\brequire\(\s*['"]/g, 4],
    [/\bmodule\.exports\b/g, 5],
    [/\bdocument\.(?:getElementById|querySelector|createElement|addEventListener)/g, 4],
    [/===|!==/g, 2, 2],
    [/\bawait\s+\w/g, 1],
    [/\bPromise\b|\.then\(/g, 2],
    [/\bwindow\./g, 2],
    [/\bJSON\.(?:parse|stringify)\(/g, 3],
    [/\.(?:map|filter|reduce|forEach)\(/g, 2],
    [/\basync\s+(?:function|\()/g, 2],
  ],
};

/** What only TypeScript has, on top of what it shares with JavaScript. */
const TYPESCRIPT_EXTRA: readonly Rule[] = [
  [/:\s*(?:string|number|boolean|any|void|unknown|never)\b(?!\s*\()/g, 3, 2],
  [/\binterface\s+\w+(?:\s+extends\s+[\w, <>]+)?\s*\{\s*\n(?:\s*\w+\??\s*:\s*[^;\n]+;?\s*\n)+/g, 6],
  [/\btype\s+\w+(?:<[^>]*>)?\s*=\s*/g, 4],
  [/\bas\s+const\b/g, 3],
  [/^\s*import\s+type\b/gm, 5],
  [/\b(?:readonly|private|public|protected)\s+\w+\??\s*:\s*\w/g, 3],
  [/\)\s*:\s*(?:void|string|number|boolean|Promise<[^>]*>)\s*(?:\{|=>)/g, 4],
  [/\bexport\s+(?:type|interface)\b/g, 5],
  [/\bReact\.FC\b/g, 3],
];

/* Languages that look alike are compared only with the others, never with
   their own family: a tie inside one goes to the first. */
const FAMILIES: readonly (readonly string[])[] = [["javascript", "typescript"], ["cpp", "c"]];

function score(text: string, rules: readonly Rule[]): number {
  let total = 0;
  for (const [pattern, points, cap = 1] of rules) {
    const found = text.match(pattern)?.length ?? 0;
    total += points * Math.min(found, cap);
  }
  return total;
}

/** The interpreter a shebang names, as a language. */
function shebangLanguage(text: string): string | null {
  const line = /^#!\s*(\S+)(?:\s+(\S+))?/.exec(text);
  if (!line) return null;
  const program = (line[1].endsWith("/env") ? line[2] : line[1])?.split("/").pop() ?? "";
  if (/^(?:ba|z|da|k)?sh$/.test(program)) return "bash";
  if (/^python[\d.]*$/.test(program)) return "python";
  if (/^(?:node|deno|bun)$/.test(program)) return "javascript";
  if (program === "ruby") return "ruby";
  if (program === "php") return "php";
  return null;
}

function json(text: string): boolean {
  if (!/^[[{]/.test(text) || !/[\]}]$/.test(text) || text.length < 7) return false;
  try {
    const value: unknown = JSON.parse(text);
    return typeof value === "object" && value !== null && (Array.isArray(value) ? value.length > 0 : Object.keys(value).length > 0);
  } catch {
    return false;
  }
}

const HTML_TAGS = /^<(?:!doctype\s+html|html|head|body|div|span|p|a|ul|ol|li|table|tr|td|th|h[1-6]|section|article|nav|header|footer|main|form|input|button|label|select|img|script|style|link|meta|title|svg|pre|code|br|hr|em|strong|b|i)\b/i;

/** Markup: HTML when it uses the tags of a web page, XML otherwise. */
function markup(text: string): "html" | "xml" | null {
  if (!/^<[a-zA-Z!?]/.test(text) || !text.endsWith(">")) return null;
  if (/^<\?xml\b/i.test(text)) return "xml";
  const tags = text.match(/<\/?[a-zA-Z][\w:.-]*(?:\s[^<>]*)?\/?>/g)?.length ?? 0;
  if (/^<!doctype\s+html/i.test(text) || /^<html\b/i.test(text)) return "html";
  if (tags < 2) return null;
  return HTML_TAGS.test(text) ? "html" : "xml";
}

/** A block of `key: value` lines, `- item` lines and comments, and nothing else. */
function yaml(text: string): boolean {
  const lines = text.split("\n").filter((line) => line.trim() && !/^\s*#/.test(line) && line.trim() !== "---");
  if (lines.length < 3) return false;
  let keys = 0;
  for (const line of lines) {
    const key = /^\s*(?:-\s+)?([\w.-]+|"[^"\n]*"|'[^'\n]*'):(?:\s+\S.*|\s*)$/.exec(line);
    if (key) {
      if (/^(?:else|try|finally)$/.test(key[1])) return false;
      keys += 1;
    } else if (!/^\s*-\s+\S/.test(line) && !/^\s+\S/.test(line)) {
      return false;
    }
  }
  return keys >= 3 && !/[{};]\s*$/m.test(text);
}

/** Text that reads as words — a note, a log, a letter — more than as code. */
function prose(text: string): boolean {
  const solid = text.replace(/\s/g, "");
  if (solid.length < 30) return false;
  const punctuation = solid.match(/[{}[\];=<>$\\|&*#@^~`]/g)?.length ?? 0;
  if (punctuation / solid.length >= 0.02) return false;
  const words = text.trim().split(/\s+/).length;
  const han = text.match(/[㐀-鿿]/g)?.length ?? 0;
  return words >= 5 || han >= 15;
}

/**
 * The language of `text` — the id of one of `CODE_LANGUAGES` — or null when it
 * is not clear. Plain text is answered only for text that plainly is prose;
 * code that is not recognised is null, not plain text.
 */
export function detectLanguage(source: string): string | null {
  const text = source.replace(/\r\n?/g, "\n").trim().slice(0, MAX_READ);
  if (!text) return null;

  const shebang = shebangLanguage(text);
  if (shebang) return shebang;
  if (json(text)) return "json";
  const tags = markup(text);
  if (tags) return tags;

  const scores = new Map<string, number>();
  for (const [language, rules] of Object.entries(RULES)) scores.set(language, score(text, rules));
  const script = scores.get("javascript") ?? 0;
  const extra = score(text, TYPESCRIPT_EXTRA);
  scores.set("typescript", extra ? script + extra : 0);

  const ranked = [...scores].filter(([, points]) => points > 0).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  const best = ranked[0];
  if (best && best[1] >= MIN_SCORE) {
    const family = FAMILIES.find((names) => names.includes(best[0]));
    const rival = ranked.find(([language]) => language !== best[0] && !family?.includes(language));
    if (!rival || (best[1] - rival[1] >= 2 && best[1] >= rival[1] * 1.5)) {
      // Inside a family: the one with more; on a tie, C++ over C and JavaScript over TypeScript.
      const inFamily = family ? ranked.filter(([language]) => family.includes(language)) : [];
      if (inFamily.length === 2 && inFamily[0][1] === inFamily[1][1]) return family!.includes("cpp") ? "cpp" : "javascript";
      return best[0];
    }
    return null;
  }
  if (yaml(text)) return "yaml";
  return prose(text) ? PLAIN_TEXT : null;
}

/** The language to give a pasted block of code, or null to leave it as it
 *  is: plain text is what a block is already. */
export function detectCodeLanguage(source: string): string | null {
  const language = detectLanguage(source);
  return language === PLAIN_TEXT ? null : language;
}
