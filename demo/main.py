import logging

import sentry_sdk
from sentry_sdk.integrations.logging import LoggingIntegration

from abcd.div import divide

# Grabbing the integration instance lets us filter its event handler below,
# so specific log calls can be excluded from becoming their own Sentry event
# (they still show up as breadcrumbs) without lowering event_level globally.
logging_integration = LoggingIntegration()

sentry_sdk.init(
    # Points at the local error-logger server instead of sentry.io.
    dsn="http://public@127.0.0.1:9000/6",
    # Set to 1.0 for demo purposes so every event is sent.
    traces_sample_rate=1.0,
    # Capture 100% of profiling data (optional).
    profile_session_sample_rate=1.0,
    profile_lifecycle="trace",
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
    print("Hello from demo!")

    # Message events at every severity level, to exercise the level-based
    # color coding in the console output and dashboard.
    # sentry_sdk.capture_message("Debug message from Python", level="debug")
    # sentry_sdk.capture_message("Info message from Python", level="info")
    # sentry_sdk.capture_message("Warning message from Python", level="warning")
    # sentry_sdk.capture_message("Error-level message from Python", level="error")
    # sentry_sdk.capture_message("Fatal message from Python", level="fatal")

    # Standard-library logging is captured automatically by sentry_sdk's
    # LoggingIntegration (enabled by default): INFO+ becomes a breadcrumb,
    # ERROR+ is sent as its own event.
    # logger.info("Info log from the stdlib logging module")
    # logger.warning("Warning log from the stdlib logging module")
    # logger.error("Error log from the stdlib logging module")

    # A handled exception, captured without crashing the process.
    divide()

    # An unhandled exception, captured automatically as the process exits.
    # raise FileExistsError("This is a demo exception!")


if __name__ == "__main__":
    main()
