# A deliberately-broken view for testing the Errgotrack integration end to
# end. Wire it up temporarily:
#
#   # urls.py
#   from .views import trigger_test_error
#   urlpatterns = [..., path("_debug/trigger-error/", trigger_test_error)]
#
# then visit /_debug/trigger-error/ with DEBUG=False (Django's own debug
# page intercepts exceptions when DEBUG=True and sentry-sdk never sees them).
# Remove the route once you've confirmed the event shows up in the dashboard.
from django.http import HttpResponse


def trigger_test_error(request):
    1 / 0
    return HttpResponse("unreachable")
