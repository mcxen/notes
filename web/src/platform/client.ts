import type { AppBootstrap, CollaboratorPresence, Person, PresenceState, SpaceMember, SpaceRoster, SpaceSummary } from "../core/types";
import { fileUrl, readFile, writeFile, type FileUrl } from './local-files';

export interface PlatformClient {
  readonly bootstrap: AppBootstrap;
  /** Who is on which page of a space: the page's presence there, once it has
   *  entered the space (`spaces.enter`). */
  presence: {
    set(state: PresenceState | null, spaceId?: string): Promise<void>;
    watch(
      listener: (presence: CollaboratorPresence[]) => void,
      spaceId?: string,
    ): Promise<() => void>;
    /** Leaves a space once the member is no longer in it. */
    release(spaceId: string): Promise<void>;
  };
  users: {
    search(query: string): Promise<Person[]>;
  };
  /** Bytes for image and file blocks: the member's private store, or the
   *  space's `SHARED_FILES_BUCKET` when `spaceId` is given. */
  files: {
    write(path: string, bytes: Blob, options: { spaceId?: string; mediaType: string }): Promise<void>;
    read(path: string, options: { spaceId?: string }): Promise<Blob>;
    /** The bytes on screen, read once a session however many views show
     *  them (the SDK's `url`), held for the view until `release()`. */
    url(path: string, options: { spaceId?: string }): FileUrl;
  };
  spaces: {
    create(name: string): Promise<{ spaceId: string; name: string }>;
    /** Every space this member belongs to that has Notes in it, with their
     *  role there and what that role lets them do. */
    installations(): Promise<SpaceSummary[]>;
    /** That list now, and again whenever it changes — a space joined (one
     *  this window made too) or left, or the member's role in one changed. */
    watchInstallations(listener: (spaces: SpaceSummary[]) => void): Promise<() => void>;
    /** Who is in the space now, read afresh. */
    members(spaceId: string): Promise<SpaceMember[]>;
    /** Who is in a space and who has been asked in — now, and again when
     *  either changes there, and only then. */
    watchRoster(spaceId: string, listener: (roster: SpaceRoster) => void): Promise<() => void>;
    invite(spaceId: string, handle: string, role: "admin" | "editor" | "viewer"): Promise<{ created: boolean }>;
    /** Owner and admins only; only the owner gives or takes Full access.
     *  Moves a member between Full access (admin), Can edit (editor) and Can
     *  view (viewer). */
    updateRole(spaceId: string, userId: string, role: "admin" | "editor" | "viewer"): Promise<void>;
    /** Owner and admins only; never the owner. Their access ends at once. */
    removeMember(spaceId: string, userId: string): Promise<void>;
    /** Anyone but the owner. */
    leave(spaceId: string): Promise<void>;
  };
  close(): Promise<void>;
}

const unsupported = async (): Promise<never> => { throw new Error('This is a personal local notebook. Online collaboration is not enabled.'); };
export async function createPlatformClient(): Promise<PlatformClient> {
  return {
    bootstrap: { appId: 'open-notes', installationId: 'local', appName: 'Open Notes', user: { id: 'local-user', handle: 'local', displayName: 'Local notebook', avatarUrl: null } },
    presence: { async set() {}, async watch(listener) { listener([]); return () => {}; }, async release() {} },
    users: { async search() { return []; } },
    files: { async write(path, bytes, options) { await writeFile(path, bytes, options.spaceId); }, async read(path, options) { return readFile(path, options.spaceId); }, url(path, options) { return fileUrl(path, options.spaceId); } },
    spaces: {
      create: unsupported,
      async installations() { return []; },
      async watchInstallations(listener) { listener([]); return () => {}; },
      async members() { return []; },
      async watchRoster(_id, listener) { listener({ members: [], invitations: [] }); return () => {}; },
      invite: unsupported, updateRole: unsupported, removeMember: unsupported, leave: unsupported,
    },
    async close() {},
  };
}
