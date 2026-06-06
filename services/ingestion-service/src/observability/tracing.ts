/* eslint-disable @typescript-eslint/no-var-requires */
/**
 * OpenTelemetry bootstrap.
 *
 * Loaded BEFORE NestJS so auto-instrumentations can patch http, kafkajs, etc.
 *
 * Activated only when OTEL_ENABLED=true (HARDNESS §8 — observability is a
 * first-class concern, but we keep startup costs optional in dev).
 */

export function startTracing(serviceName: string, serviceVersion = '0.1.0'): void {
  if (process.env.OTEL_ENABLED !== 'true') {
    return;
  }

  // Lazy require so the SDK packages are only loaded when tracing is on.
  const { NodeSDK } = require('@opentelemetry/sdk-node');
  const {
    getNodeAutoInstrumentations,
  } = require('@opentelemetry/auto-instrumentations-node');
  const {
    OTLPTraceExporter,
  } = require('@opentelemetry/exporter-trace-otlp-http');
  const { Resource } = require('@opentelemetry/resources');
  const {
    SemanticResourceAttributes,
  } = require('@opentelemetry/semantic-conventions');

  const sdk = new NodeSDK({
    resource: new Resource({
      [SemanticResourceAttributes.SERVICE_NAME]: serviceName,
      [SemanticResourceAttributes.SERVICE_VERSION]: serviceVersion,
    }),
    traceExporter: new OTLPTraceExporter({
      url: `${process.env.OTEL_EXPORTER_OTLP_ENDPOINT ?? 'http://localhost:4318'}/v1/traces`,
    }),
    instrumentations: [getNodeAutoInstrumentations()],
  });

  sdk.start();

  process.on('SIGTERM', () => {
    sdk.shutdown().catch(() => undefined);
  });
}
