/**
 * Workspace — tenant unit of the platform.
 *
 * Each workspace has a unique public URL for receiving webhooks:
 *   https://domain.com/{workspaceId}/{endpointToken}
 */
export interface Workspace {
  /** UUID v4 — primary key. */
  readonly id: string;

  /** Human-readable display name. */
  readonly name: string;

  /**
   * URL-safe token embedded in the inbound webhook URL.
   * Serves as a simple bearer credential — keep it secret.
   */
  readonly endpointToken: string;

  readonly createdAt: Date;
  readonly updatedAt: Date;
}
