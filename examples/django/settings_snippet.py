# Add this to your Django project's settings.py (or a settings/production.py
# that's only imported in deployed environments). Requires: pip install sentry-sdk
import os
import sentry_sdk
from sentry_sdk.integrations.django import DjangoIntegration

sentry_sdk.init(
    dsn=os.environ["SENTRY_DSN"],  # copy this from the project's page in Errgotrack
    environment=os.getenv("SENTRY_ENVIRONMENT", "production"),
    release=os.getenv("APP_RELEASE"),
    integrations=[DjangoIntegration()],
    send_default_pii=False,       # don't attach request/user PII unless you've reviewed what that means for you
    traces_sample_rate=0.0,       # performance tracing isn't implemented server-side yet; leave this at 0
)
