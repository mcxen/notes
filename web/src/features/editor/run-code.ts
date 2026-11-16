import { isMyGo, runtime } from "mygo-runtime";
import { Notebooks } from "../../platform/mygo";
import { codeLanguage } from "./code-highlight";

/* A Bash code block can be run: on a Mac, in the app, its commands go to the
   system's terminal. The app asks first, in a dialog of the system's that
   shows the commands — the page only asks, it cannot answer. In a browser,
   and on Windows and Linux, a code block has no Run button. */

/** Whether a code block in this language has a Run button here. */
export function runsInTerminal(language: string | undefined): boolean {
  return codeLanguage(language).id === "bash" && isMyGo() && runtime().platform === "darwin";
}

/** Asks the app to run the code in the terminal. Resolves when it was handed
 *  over, and when the user said no; rejects when it could not be run. */
export function runInTerminal(code: string): Promise<void> {
  return Notebooks.runInTerminal(code);
}
