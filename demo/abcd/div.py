import logging

logger = logging.getLogger(__name__)

def divide():
    try:
        1 / 0
    except ZeroDivisionError:
        logger.exception("Division failed inside divide()")

    # An unhandled exception, captured automatically as the process exits.
    raise FileExistsError("This is a demo exception!")