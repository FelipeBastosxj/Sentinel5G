/**
 * Structured JSON logger.
 *
 * HARDNESS §8 mandates structured logs with correlation-ID propagation.
 * This implementation is intentionally tiny so services depend on a thin,
 * stable abstraction rather than a heavy logging framework. It writes a
 * single JSON line per record to stdout/stderr so log shippers (Loki,
 * Fluent Bit, Vector) can ingest without parsing rules.
 */
export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

export interface LogFields {
  readonly correlationId?: string;
  readonly [key: string]: unknown;
}

export interface StructuredLogger {
  debug(message: string, fields?: LogFields): void;
  info(message: string, fields?: LogFields): void;
  warn(message: string, fields?: LogFields): void;
  error(message: string, fields?: LogFields): void;
  child(bindings: LogFields): StructuredLogger;
}

const LEVEL_RANK: Record<LogLevel, number> = {
  debug: 10,
  info: 20,
  warn: 30,
  error: 40,
};

export interface CreateLoggerOptions {
  readonly service: string;
  readonly level?: LogLevel;
  readonly base?: LogFields;
}

export function createLogger(options: CreateLoggerOptions): StructuredLogger {
  const minLevel = LEVEL_RANK[options.level ?? 'info'];
  const baseBindings: LogFields = { service: options.service, ...options.base };

  function emit(level: LogLevel, message: string, fields?: LogFields): void {
    if (LEVEL_RANK[level] < minLevel) return;
    const record = {
      level,
      time: new Date().toISOString(),
      message,
      ...baseBindings,
      ...fields,
    };
    const line = JSON.stringify(record);
    if (level === 'error' || level === 'warn') {
      // eslint-disable-next-line no-console
      console.error(line);
    } else {
      // eslint-disable-next-line no-console
      console.log(line);
    }
  }

  return {
    debug: (m, f) => emit('debug', m, f),
    info: (m, f) => emit('info', m, f),
    warn: (m, f) => emit('warn', m, f),
    error: (m, f) => emit('error', m, f),
    child(bindings: LogFields): StructuredLogger {
      return createLogger({
        service: options.service,
        level: options.level,
        base: { ...baseBindings, ...bindings },
      });
    },
  };
}
