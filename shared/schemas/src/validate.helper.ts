import type { ZodSchema } from 'zod';

/**
 * Domain-level error raised when a payload fails schema validation.
 * Services translate this into a transport-specific 4xx response.
 */
export class SchemaValidationError extends Error {
  public readonly issues: readonly { path: string; message: string }[];

  constructor(issues: readonly { path: string; message: string }[]) {
    super(`Schema validation failed: ${issues.length} issue(s)`);
    this.name = 'SchemaValidationError';
    this.issues = issues;
  }
}

/**
 * Validates a value against a Zod schema and returns the parsed value.
 * Throws {@link SchemaValidationError} on failure.
 */
export function validateOrThrow<T>(schema: ZodSchema<T>, value: unknown): T {
  const result = schema.safeParse(value);
  if (!result.success) {
    throw new Error(
      `Validation failed: ${JSON.stringify(result.error.issues)}`,
    );
  }
  return result.data;
}
