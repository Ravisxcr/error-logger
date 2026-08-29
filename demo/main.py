import logging
import random
import time
import sentry_sdk
from sentry_sdk import (
    add_breadcrumb,
    capture_exception,
    capture_message,
    isolation_scope,
    set_tag,
    set_user,
)

# 1. Setup standard logging integration (Sentry auto-captures these as breadcrumbs)
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("load_tester")

# 2. Initialize Sentry SDK
sentry_sdk.init(
    dsn="http://public@localhost:9000/100",  # <-- Replace with your actual Sentry DSN
    traces_sample_rate=1.0,
    environment="load-test",
    release="sentry-tester@1.0.0",
    max_breadcrumbs=50,
)

# 3. Global Context & Tags
set_tag("app_component", "event_generator")
set_user({"id": "usr_99", "email": "tester@example.com", "username": "load_bot"})


# --- Error Generating Scenarios ---

def scenario_zero_division():
    add_breadcrumb(
        category="calc",
        message="Performing calculation: ratio split",
        level="info",
        data={"divisor": 0},
    )
    logger.info("Evaluating denominator...")
    return 100 / 0


def scenario_index_error():
    add_breadcrumb(
        category="cache",
        message="Reading memory buffer slice",
        level="debug",
        data={"requested_index": 15, "buffer_len": 3},
    )
    logger.warning("Cache miss; attempting direct buffer lookup")
    data = ["chunk_a", "chunk_b", "chunk_c"]
    return data[15]


def scenario_business_logic():
    add_breadcrumb(
        category="auth",
        message="Token verified via OAuth provider",
        level="info",
        data={"provider": "github", "scope": "read:org"},
    )
    logger.info("Validating payment method...")
    add_breadcrumb(
        category="billing",
        message="Card decline: insufficient funds",
        level="warning",
        data={"attempt": 3, "gateway": "stripe"},
    )
    raise ValueError("Transaction failed: billing attempt threshold exceeded")


def scenario_captured_message(iteration: int):
    levels = ["info", "warning", "error", "fatal"]
    chosen_level = random.choice(levels)

    add_breadcrumb(
        category="system",
        message="Heartbeat health check initiated",
        level="debug",
    )
    logger.info(f"Emitting synthetic telemetry log for batch item #{iteration}")

    capture_message(
        f"Synthetic log event #{iteration} fired with level: {chosen_level.upper()}",
        level=chosen_level,
    )


# --- Runner ---

def run_sentry_test(total_events: int = 20, delay_seconds: float = 0.15):
    error_scenarios = [
        scenario_zero_division,
        scenario_index_error,
        scenario_business_logic,
    ]

    print(f"Generating {total_events} events with rich breadcrumbs...\n")

    for i in range(1, total_events + 1):
        # Isolate scope so breadcrumbs from previous loop iterations don't bleed over
        with isolation_scope():
            set_tag("iteration_id", f"run_{i}")

            # 60% chance of an exception, 40% chance of a captured message
            if random.random() < 0.6:
                chosen_scenario = random.choice(error_scenarios)
                try:
                    chosen_scenario()
                except Exception as exc:
                    capture_exception(exc)
                    print(f"[{i:02d}/{total_events:02d}] Captured Exception: {type(exc).__name__}")
            else:
                scenario_captured_message(iteration=i)
                print(f"[{i:02d}/{total_events:02d}] Captured Message Event")

        time.sleep(delay_seconds)

    # Flush all buffered network requests before exiting
    sentry_sdk.flush(timeout=5)
    print("\nBatch complete. Check your Sentry dashboard issues and breadcrumb timelines.")


if __name__ == "__main__":
    run_sentry_test(total_events=20, delay_seconds=0.1)