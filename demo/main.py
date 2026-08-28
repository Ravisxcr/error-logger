import logging
import os

import sentry_sdk
from sentry_sdk.integrations.logging import LoggingIntegration

from abcd.div import divide

dsn = os.getenv("SENTRY_DSN", "http://public@127.0.0.1:9000/1")

# Grabbing the integration instance lets us filter its event handler below,
# so specific log calls can be excluded from becoming their own Sentry event
# (they still show up as breadcrumbs) without lowering event_level globally.
logging_integration = LoggingIntegration()

sentry_sdk.init(
    # Points at the local error-logger server instead of sentry.io.
    dsn=dsn,
    # Set to 1.0 for demo purposes so every event is sent.
    traces_sample_rate=1.0,
    # Capture 100% of profiling data (optional).
    profile_session_sample_rate=1.0,
    profile_lifecycle="trace",
    environment="production",
    release="python-demo@1.0.0",
    integrations=[logging_integration],
)


class SkipSentryEventFilter(logging.Filter):
    def filter(self, record: logging.LogRecord) -> bool:
        return not getattr(record, "skip_sentry_event", False)


# logging_integration._handler is the standard logging.Handler that turns
# ERROR+ records into events; addFilter is plain stdlib logging API.
logging_integration._handler.addFilter(SkipSentryEventFilter())

logger = logging.getLogger(__name__)


def main():
    print("Hello from Python Sentry demo!")

    # Standard-library logging is captured automatically by sentry_sdk's
    # LoggingIntegration: INFO+ becomes breadcrumbs, ERROR+ becomes an event.
    logger.info("Info log from the stdlib logging module")

    # A handled exception, captured without crashing the process.
    try:
        divide()
    except Exception as exc:
        print(f"Caught exception in demo runner: {exc}")

    # Flush events before exiting
    sentry_sdk.flush(timeout=2.0)
    print("Python demo finished successfully.")


if __name__ == "__main__":
    main()
