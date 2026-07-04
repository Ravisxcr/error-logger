import sentry_sdk

def divide():
    try:
        1 / 0
    except ZeroDivisionError:
        sentry_sdk.capture_exception()

    # An unhandled exception, captured automatically as the process exits.
    raise FileExistsError("This is a demo exception!")