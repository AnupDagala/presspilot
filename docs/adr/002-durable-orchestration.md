# ADR 002 · Durable orchestration and outbox

Accepted. Temporal owns approval waits, bounded retries and recovery. Activities own nondeterminism. The case row doubles as the dispatch outbox to avoid another queue service. Stable workflow IDs close the dispatch/acknowledgement gap.

Approval polling every five seconds uses durable timers and authenticated reads. It introduces several seconds of completion latency and extra bounded history. Signals are a future optimization; they would also need an outbox so an approval commit cannot lose its notification. Provider delivery ambiguity escalates conservatively; business mutation ambiguity reconciles through the execution record.
