// Package platforms holds the per-language/framework setup snippets shown
// on a project's page. The ingestion server itself doesn't need to know
// what framework sent an event - the Sentry wire protocol is identical
// across every official SDK - so this package is purely about giving the
// person setting up a new project copy-pasteable, framework-correct install
// instructions instead of one hardcoded Django example.
package platforms

import "fmt"

type Platform struct {
	ID      string
	Label   string
	Install string
	// Snippet renders the init code for this platform given the project's
	// DSN and default environment.
	Snippet func(dsn, env string) string
}

var All = []Platform{
	{
		ID:      "python-django",
		Label:   "Python - Django",
		Install: "pip install sentry-sdk",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`import os
import sentry_sdk
from sentry_sdk.integrations.django import DjangoIntegration

sentry_sdk.init(
    dsn=os.environ["SENTRY_DSN"],
    environment=os.getenv("SENTRY_ENVIRONMENT", %q),
    integrations=[DjangoIntegration()],
    send_default_pii=False,
    traces_sample_rate=0.0,
)`, env)
		},
	},
	{
		ID:      "python-flask",
		Label:   "Python - Flask",
		Install: "pip install sentry-sdk",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`import os
import sentry_sdk
from sentry_sdk.integrations.flask import FlaskIntegration

sentry_sdk.init(
    dsn=os.environ["SENTRY_DSN"],
    environment=os.getenv("SENTRY_ENVIRONMENT", %q),
    integrations=[FlaskIntegration()],
    send_default_pii=False,
    traces_sample_rate=0.0,
)

app = Flask(__name__)`, env)
		},
	},
	{
		ID:      "python-fastapi",
		Label:   "Python - FastAPI",
		Install: "pip install sentry-sdk",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`import os
import sentry_sdk

# sentry_sdk auto-detects FastAPI/Starlette - no explicit integration needed,
# just call init() before creating the app.
sentry_sdk.init(
    dsn=os.environ["SENTRY_DSN"],
    environment=os.getenv("SENTRY_ENVIRONMENT", %q),
    send_default_pii=False,
    traces_sample_rate=0.0,
)

app = FastAPI()`, env)
		},
	},
	{
		ID:      "nodejs",
		Label:   "Node.js",
		Install: "npm install @sentry/node",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`// Import and call Sentry.init as the very first thing in your entrypoint.
import * as Sentry from "@sentry/node";

Sentry.init({
  dsn: process.env.SENTRY_DSN,
  environment: process.env.SENTRY_ENVIRONMENT || %q,
  tracesSampleRate: 0,
});`, env)
		},
	},
	{
		ID:      "bun",
		Label:   "Bun",
		Install: "bun add @sentry/bun",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`// Import and call Sentry.init as the very first thing in your entrypoint.
import * as Sentry from "@sentry/bun";

Sentry.init({
  dsn: Bun.env.SENTRY_DSN,
  environment: Bun.env.SENTRY_ENVIRONMENT || %q,
  tracesSampleRate: 0,
});`, env)
		},
	},
	{
		ID:      "nextjs",
		Label:   "Next.js",
		Install: "npx @sentry/wizard@latest -i nextjs",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`// The wizard generates sentry.server.config.ts / sentry.client.config.ts /
// sentry.edge.config.ts for you. If wiring it up by hand instead, each one
// needs at minimum:
import * as Sentry from "@sentry/nextjs";

Sentry.init({
  dsn: process.env.NEXT_PUBLIC_SENTRY_DSN,
  environment: process.env.NODE_ENV || %q,
  tracesSampleRate: 0,
});`, env)
		},
	},
	{
		ID:      "javascript-browser",
		Label:   "JavaScript (browser)",
		Install: "npm install @sentry/browser",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`import * as Sentry from "@sentry/browser";

Sentry.init({
  dsn: %q,
  environment: %q,
  tracesSampleRate: 0,
});`, dsn, env)
		},
	},
	{
		ID:    "java-spring-boot",
		Label: "Java - Spring Boot",
		Install: `<dependency>
  <groupId>io.sentry</groupId>
  <artifactId>sentry-spring-boot-starter-jakarta</artifactId>
  <version>7.+</version>
</dependency>`,
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`# application.properties - the starter auto-configures everything else.
sentry.dsn=%s
sentry.environment=%s
sentry.traces-sample-rate=0`, dsn, env)
		},
	},
	{
		ID:      "java-quarkus",
		Label:   "Java - Quarkus",
		Install: "./mvnw quarkus:add-extension -Dextensions=\"quarkus-sentry\"",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`# application.properties
quarkus.sentry.dsn=%s
quarkus.sentry.environment=%s
quarkus.sentry.traces-sample-rate=0`, dsn, env)
		},
	},
	{
		ID:      "java-other",
		Label:   "Java (other / plain JVM)",
		Install: "Download io.sentry:sentry from Maven Central (no other runtime dependency required).",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`import io.sentry.Sentry;

Sentry.init(options -> {
    options.setDsn(%q);
    options.setEnvironment(%q);
    options.setTracesSampleRate(0.0);
});

try {
    // your code
} catch (Exception e) {
    Sentry.captureException(e);
}`, dsn, env)
		},
	},
	{
		ID:      "other",
		Label:   "Other / generic Sentry SDK",
		Install: "Install whichever official Sentry SDK matches your stack.",
		Snippet: func(dsn, env string) string {
			return fmt.Sprintf(`DSN: %s
Environment: %s

Any official Sentry SDK works: it talks the same envelope protocol
regardless of language. Point it at the DSN above.`, dsn, env)
		},
	},
}

var byID = func() map[string]Platform {
	m := make(map[string]Platform, len(All))
	for _, p := range All {
		m[p.ID] = p
	}
	return m
}()

const DefaultID = "python-django"

// Get returns the platform for id, falling back to the default if id is
// empty or unrecognized (e.g. an older project row from before this field
// existed).
func Get(id string) Platform {
	if p, ok := byID[id]; ok {
		return p
	}
	return byID[DefaultID]
}

func Valid(id string) bool {
	_, ok := byID[id]
	return ok
}
