import { Notebooks } from "../../platform/mygo";
import { base64Of } from "../../platform/local-files";
import type { ShareTransport } from "./publisher";

/** The worker as the app reaches it: through Go, which holds the token and
 *  keeps where each page is published. */
export const goTransport: ShareTransport = {
  check: () => Notebooks.shareCheck(),
  publish: async (pageId, id, page) => {
    const reply = await Notebooks.sharePublish(pageId, id, {
      title: page.title,
      html: page.html,
      icon: page.icon,
      parent: page.parent,
      position: page.position,
      files: page.files,
    });
    return { id: reply.id, url: reply.url, missing: reply.missing ?? [] };
  },
  upload: async (id, name, type, bytes) => {
    await Notebooks.shareUpload(id, name, type, await base64Of(new Blob([bytes as BlobPart])));
  },
  unpublish: (pageId) => Notebooks.shareUnpublish(pageId),
};
