/**
 * Channel — Communication channel through which the event flows.
 *
 * Defined by the MASTER_PROMPT and enforced by HARDNESS §4 (Canonical Event Rule).
 * Initial development focuses on the SMS channel; other channels are scaffolded
 * for forward-compatibility.
 */
export enum Channel {
  SMS = 'sms',
  EMAIL = 'email',
  WHATSAPP = 'whatsapp',
  PUSH = 'push',
  INTERNAL = 'internal',
}

/**
 * Type guard for runtime channel validation.
 */
export function isChannel(value: unknown): value is Channel {
  return (
    typeof value === 'string' &&
    (Object.values(Channel) as string[]).includes(value)
  );
}
