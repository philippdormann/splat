// Package platforms holds the per-language/framework setup snippets shown
// on a project's page. The ingestion server itself doesn't need to know
// what framework sent an event - the Sentry wire protocol is identical
// across every official SDK - so this package is purely about giving the
// person setting up a new project copy-pasteable, framework-correct install
// instructions instead of one hardcoded Django example.
//
// The ID list mirrors rustrak's SELECTABLE_PLATFORMS (itself generated from
// Sentry's platforms.tsx plus Relay's VALID_PLATFORMS), so anything
// auto-detection can write stays selectable here too.
package platforms

import "fmt"

type Platform struct {
	ID       string
	Label    string
	Language string
	Install  string
	// Snippet renders the init code for this platform given the project's
	// DSN and default environment.
	Snippet func(dsn, env string) string
}

// Family snippet builders: one per SDK language. Framework-specific entries
// below reuse these with a tailored package/import line; obscure runtimes
// fall back to generic() which still gives a working DSN pointer.

func pythonSnippet(integrationNote, extra string) func(dsn, env string) string {
	return func(dsn, env string) string {
		s := `import os
import sentry_sdk
`
		if integrationNote != "" {
			s += integrationNote + "\n"
		}
		s += fmt.Sprintf(`sentry_sdk.init(
    dsn=os.environ["SENTRY_DSN"],
    environment=os.getenv("SENTRY_ENVIRONMENT", %q),
    send_default_pii=False,
    traces_sample_rate=0.0,
)`, env)
		if extra != "" {
			s += "\n\n" + extra
		}
		return s
	}
}

func nodeSnippet(pkg, envExpr string) func(dsn, env string) string {
	return func(dsn, env string) string {
		return fmt.Sprintf(`// Import and call Sentry.init as the very first thing in your entrypoint.
import * as Sentry from %q;

Sentry.init({
  dsn: %s,
  environment: %s || %q,
  tracesSampleRate: 0,
});`, pkg, envExpr, envExpr, env)
	}
}

func browserSnippet(pkg string) func(dsn, env string) string {
	return func(dsn, env string) string {
		return fmt.Sprintf(`import * as Sentry from %q;

Sentry.init({
  dsn: %q,
  environment: %q,
  tracesSampleRate: 0,
});`, pkg, dsn, env)
	}
}

func goSnippet(pkg string) func(dsn, env string) string {
	return func(dsn, env string) string {
		return fmt.Sprintf(`import "github.com/getsentry/sentry-go"

err := sentry.Init(sentry.ClientOptions{
	Dsn:         %q,
	Environment: %q,
})
if err != nil {
	// log the init failure; the app keeps running without reporting
}`, dsn, env) + "\n// Package: " + pkg
	}
}

func genericSnippet(sdk, installHint string) func(dsn, env string) string {
	return func(dsn, env string) string {
		return fmt.Sprintf(`DSN: %s
Environment: %s

Install the official %s Sentry SDK (%s) and point it at the DSN above.
Any official Sentry SDK speaks the same envelope protocol, so events
arrive regardless of language.`, dsn, env, sdk, installHint)
	}
}

func def(id, label, language, install string, snippet func(dsn, env string) string) Platform {
	return Platform{ID: id, Label: label, Language: language, Install: install, Snippet: snippet}
}

var All = []Platform{
	def("android", "Android", "android", "implementation 'io.sentry:sentry-android:7.+'",
		genericSnippet("Android", "io.sentry:sentry-android")),
	def("apple", "Apple", "apple", "Swift Package Manager: github.com/getsentry/sentry-cocoa",
		genericSnippet("Apple", "sentry-cocoa")),
	def("apple-ios", "iOS", "apple", "Swift Package Manager: github.com/getsentry/sentry-cocoa",
		genericSnippet("iOS", "sentry-cocoa")),
	def("apple-macos", "macOS", "apple", "Swift Package Manager: github.com/getsentry/sentry-cocoa",
		genericSnippet("macOS", "sentry-cocoa")),
	def("as3", "ActionScript", "as3", "Install the Sentry ActionScript SDK.",
		genericSnippet("ActionScript", "sentry-as3")),
	def("bun", "Bun", "bun", "bun add @sentry/bun",
		nodeSnippet("@sentry/bun", "Bun.env.SENTRY_DSN")),
	def("c", "C", "c", "Add sentry-native to your build (CMake FetchContent or vcpkg).",
		func(dsn, env string) string {
			return fmt.Sprintf(`// sentry-native
sentry_options_t *options = sentry_options_new();
sentry_options_set_dsn(options, %q);
sentry_options_set_environment(options, %q);
sentry_options_set_release(options, "my-app@1.0.0");
sentry_init(options);`, dsn, env)
		}),
	def("capacitor", "Capacitor", "capacitor", "npm install @sentry/capacitor",
		browserSnippet("@sentry/capacitor")),
	def("cfml", "ColdFusion", "cfml", "Install the Sentry CFML SDK.",
		genericSnippet("ColdFusion", "sentry-cfml")),
	def("cocoa", "Cocoa", "cocoa", "Swift Package Manager: github.com/getsentry/sentry-cocoa",
		genericSnippet("Cocoa", "sentry-cocoa")),
	def("cordova", "Cordova", "cordova", "npm install @sentry/cordova",
		browserSnippet("@sentry/cordova")),
	def("csharp", "C#", "csharp", "dotnet add package Sentry",
		func(dsn, env string) string {
			return fmt.Sprintf(`using Sentry;

SentrySdk.Init(o => {
    o.Dsn = %q;
    o.Environment = %q;
});`, dsn, env)
		}),
	def("dart", "Dart", "dart", "flutter pub add sentry (pure Dart: dart pub add sentry)",
		func(dsn, env string) string {
			return fmt.Sprintf(`import 'package:sentry/sentry.dart';

await Sentry.init((o) => {
  o.dsn = %q;
  o.environment = %q;
});`, dsn, env)
		}),
	def("deno", "Deno", "deno", "npm:@sentry/deno or import from esm.sh",
		func(dsn, env string) string {
			return fmt.Sprintf(`import * as Sentry from "npm:@sentry/deno";

Sentry.init({
  dsn: %q,
  environment: %q,
  tracesSampleRate: 0,
});`, dsn, env)
		}),
	def("dotnet", ".NET", "dotnet", "dotnet add package Sentry",
		func(dsn, env string) string {
			return fmt.Sprintf(`using Sentry;

SentrySdk.Init(o => {
    o.Dsn = %q;
    o.Environment = %q;
});`, dsn, env)
		}),
	def("dotnet-aspnet", "ASP.NET", "dotnet", "dotnet add package Sentry.AspNet",
		genericSnippet("ASP.NET", "Sentry.AspNet")),
	def("dotnet-aspnetcore", "ASP.NET Core", "dotnet", "dotnet add package Sentry.AspNetCore",
		func(dsn, env string) string {
			return fmt.Sprintf(`// Program.cs
builder.WebHost.UseSentry(o => {
    o.Dsn = %q;
    o.Environment = %q;
});`, dsn, env)
		}),
	def("dotnet-awslambda", "AWS Lambda (.NET)", "dotnet", "dotnet add package Sentry.AwsLambda",
		genericSnippet("AWS Lambda (.NET)", "Sentry.AwsLambda")),
	def("dotnet-gcpfunctions", "Google Cloud Functions (.NET)", "dotnet", "dotnet add package Sentry",
		genericSnippet("Google Cloud Functions (.NET)", "Sentry")),
	def("dotnet-maui", ".NET MAUI", "dotnet", "dotnet add package Sentry.Maui",
		genericSnippet(".NET MAUI", "Sentry.Maui")),
	def("dotnet-uwp", "UWP", "dotnet", "dotnet add package Sentry",
		genericSnippet("UWP", "Sentry")),
	def("dotnet-winforms", "Windows Forms", "dotnet", "dotnet add package Sentry.WinForms",
		genericSnippet("Windows Forms", "Sentry.WinForms")),
	def("dotnet-wpf", "WPF", "dotnet", "dotnet add package Sentry.Wpf",
		genericSnippet("WPF", "Sentry.Wpf")),
	def("dotnet-xamarin", "Xamarin", "dotnet", "Install Sentry.Xamarin from NuGet.",
		genericSnippet("Xamarin", "Sentry.Xamarin")),
	def("electron", "Electron", "electron", "npm install @sentry/electron",
		nodeSnippet("@sentry/electron", "process.env.SENTRY_DSN")),
	def("elixir", "Elixir", "elixir", "Add {:sentry, \"~> 10.0\"} to mix.exs deps",
		func(dsn, env string) string {
			return fmt.Sprintf(`# config/runtime.exs
config :sentry,
  dsn: %q,
  environment_name: %q,
  included_environments: [%q]`, dsn, env, env)
		}),
	def("flutter", "Flutter", "flutter", "flutter pub add sentry",
		func(dsn, env string) string {
			return fmt.Sprintf(`import 'package:sentry_flutter/sentry_flutter.dart';

await SentryFlutter.init((o) => {
  o.dsn = %q;
  o.environment = %q;
});`, dsn, env)
		}),
	def("go", "Go", "go", "go get github.com/getsentry/sentry-go",
		goSnippet("github.com/getsentry/sentry-go")),
	def("go-echo", "Echo", "go", "go get github.com/getsentry/sentry-go",
		goSnippet("github.com/getsentry/sentry-go // + sentryecho integration")),
	def("go-fasthttp", "FastHTTP", "go", "go get github.com/getsentry/sentry-go",
		goSnippet("github.com/getsentry/sentry-go // + sentryfasthttp integration")),
	def("go-fiber", "Fiber", "go", "go get github.com/getsentry/sentry-go",
		goSnippet("github.com/getsentry/sentry-go // + sentryfiber integration")),
	def("go-gin", "Gin", "go", "go get github.com/getsentry/sentry-go",
		goSnippet("github.com/getsentry/sentry-go // + sentrygin integration")),
	def("go-http", "Net/Http", "go", "go get github.com/getsentry/sentry-go",
		goSnippet("github.com/getsentry/sentry-go // + sentryhttp integration")),
	def("go-iris", "Iris", "go", "go get github.com/getsentry/sentry-go",
		goSnippet("github.com/getsentry/sentry-go // + sentryiris integration")),
	def("go-martini", "Martini", "go", "go get github.com/getsentry/sentry-go",
		goSnippet("github.com/getsentry/sentry-go")),
	def("go-negroni", "Negroni", "go", "go get github.com/getsentry/sentry-go",
		goSnippet("github.com/getsentry/sentry-go // + sentrynegroni integration")),
	def("godot", "Godot", "godot", "Add the sentry-godot plugin to your project.",
		genericSnippet("Godot", "sentry-godot")),
	def("groovy", "Groovy", "groovy", "implementation 'io.sentry:sentry:7.+'",
		genericSnippet("Groovy", "io.sentry:sentry")),
	def("haskell", "Haskell", "haskell", "Add raven-haskell / sentry to your cabal file.",
		genericSnippet("Haskell", "sentry")),
	def("ionic", "Ionic", "ionic", "npm install @sentry/capacitor (Ionic + Capacitor)",
		browserSnippet("@sentry/capacitor")),
	def("java", "Java", "java", "Download io.sentry:sentry from Maven Central.",
		func(dsn, env string) string {
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
		}),
	def("java-log4j2", "Log4j 2.x", "java", "Add io.sentry:sentry-log4j2 to your pom.xml.",
		genericSnippet("Log4j 2.x", "io.sentry:sentry-log4j2")),
	def("java-logback", "Logback", "java", "Add io.sentry:sentry-logback to your pom.xml.",
		genericSnippet("Logback", "io.sentry:sentry-logback")),
	def("java-spring", "Spring", "java", "Add io.sentry:sentry-spring to your pom.xml.",
		genericSnippet("Spring", "io.sentry:sentry-spring")),
	def("java-spring-boot", "Spring Boot", "java", `<dependency>
  <groupId>io.sentry</groupId>
  <artifactId>sentry-spring-boot-starter-jakarta</artifactId>
  <version>7.+</version>
</dependency>`,
		func(dsn, env string) string {
			return fmt.Sprintf(`# application.properties - the starter auto-configures everything else.
sentry.dsn=%s
sentry.environment=%s
sentry.traces-sample-rate=0`, dsn, env)
		}),
	def("java-quarkus", "Quarkus", "java", "./mvnw quarkus:add-extension -Dextensions=\"quarkus-sentry\"",
		func(dsn, env string) string {
			return fmt.Sprintf(`# application.properties
quarkus.sentry.dsn=%s
quarkus.sentry.environment=%s
quarkus.sentry.traces-sample-rate=0`, dsn, env)
		}),
	def("javascript", "Browser JavaScript", "javascript", "npm install @sentry/browser",
		browserSnippet("@sentry/browser")),
	def("javascript-angular", "Angular", "javascript", "npm install @sentry/angular",
		browserSnippet("@sentry/angular")),
	def("javascript-astro", "Astro", "javascript", "npm install @sentry/astro",
		browserSnippet("@sentry/astro")),
	def("javascript-ember", "Ember", "javascript", "npm install @sentry/ember",
		browserSnippet("@sentry/ember")),
	def("javascript-gatsby", "Gatsby", "javascript", "npm install @sentry/gatsby",
		browserSnippet("@sentry/gatsby")),
	def("javascript-nextjs", "Next.js", "javascript", "npx @sentry/wizard@latest -i nextjs",
		func(dsn, env string) string {
			return fmt.Sprintf(`// The wizard generates sentry.server.config.ts / sentry.client.config.ts /
// sentry.edge.config.ts for you. If wiring it up by hand instead, each one
// needs at minimum:
import * as Sentry from "@sentry/nextjs";

Sentry.init({
  dsn: process.env.NEXT_PUBLIC_SENTRY_DSN,
  environment: process.env.NODE_ENV || %q,
  tracesSampleRate: 0,
});`, env)
		}),
	def("javascript-nuxt", "Nuxt", "javascript", "npm install @sentry/nuxt",
		browserSnippet("@sentry/nuxt")),
	def("javascript-react", "React", "javascript", "npm install @sentry/react",
		browserSnippet("@sentry/react")),
	def("javascript-react-router", "React Router Framework", "javascript", "npm install @sentry/react",
		browserSnippet("@sentry/react")),
	def("javascript-remix", "Remix", "javascript", "npm install @sentry/remix",
		browserSnippet("@sentry/remix")),
	def("javascript-solid", "Solid", "javascript", "npm install @sentry/solid",
		browserSnippet("@sentry/solid")),
	def("javascript-solidstart", "SolidStart", "javascript", "npm install @sentry/solidstart",
		browserSnippet("@sentry/solidstart")),
	def("javascript-svelte", "Svelte", "javascript", "npm install @sentry/svelte",
		browserSnippet("@sentry/svelte")),
	def("javascript-sveltekit", "SvelteKit", "javascript", "npm install @sentry/sveltekit",
		browserSnippet("@sentry/sveltekit")),
	def("javascript-tanstackstart-react", "TanStack Start React", "javascript", "npm install @sentry/tanstackstart-react",
		browserSnippet("@sentry/tanstackstart-react")),
	def("javascript-vue", "Vue", "javascript", "npm install @sentry/vue",
		browserSnippet("@sentry/vue")),
	def("kotlin", "Kotlin", "kotlin", "implementation 'io.sentry:sentry:7.+'",
		genericSnippet("Kotlin", "io.sentry:sentry")),
	def("minidump", "Minidump", "minidump", "Upload minidumps via the envelope endpoint.",
		genericSnippet("Minidump", "minidump upload")),
	def("native", "Native", "native", "Add sentry-native to your build.",
		genericSnippet("Native", "sentry-native")),
	def("native-qt", "Qt", "native", "Add sentry-native to your Qt build.",
		genericSnippet("Qt", "sentry-native")),
	def("nintendo-switch", "Nintendo Switch", "console", "Contact Sentry for the console SDK.",
		genericSnippet("Nintendo Switch", "console SDK")),
	def("node", "Node.js", "node", "npm install @sentry/node",
		nodeSnippet("@sentry/node", "process.env.SENTRY_DSN")),
	def("node-awslambda", "AWS Lambda (Node)", "node", "npm install @sentry/aws-serverless",
		nodeSnippet("@sentry/aws-serverless", "process.env.SENTRY_DSN")),
	def("node-azurefunctions", "Azure Functions (Node)", "node", "npm install @sentry/node",
		nodeSnippet("@sentry/node", "process.env.SENTRY_DSN")),
	def("node-cloudflare-pages", "Cloudflare Pages", "node", "npm install @sentry/cloudflare",
		nodeSnippet("@sentry/cloudflare", "process.env.SENTRY_DSN")),
	def("node-cloudflare-workers", "Cloudflare Workers", "node", "npm install @sentry/cloudflare",
		nodeSnippet("@sentry/cloudflare", "process.env.SENTRY_DSN")),
	def("node-connect", "Connect", "node", "npm install @sentry/node",
		nodeSnippet("@sentry/node", "process.env.SENTRY_DSN")),
	def("node-express", "Express", "node", "npm install @sentry/node",
		func(dsn, env string) string {
			return fmt.Sprintf(`import * as Sentry from "@sentry/node";
import express from "express";

Sentry.init({
  dsn: process.env.SENTRY_DSN,
  environment: process.env.SENTRY_ENVIRONMENT || %q,
  tracesSampleRate: 0,
});

const app = express();
// Sentry error handler must be the last middleware:
app.use(Sentry.expressErrorHandler());`, env)
		}),
	def("node-fastify", "Fastify", "node", "npm install @sentry/node",
		nodeSnippet("@sentry/node", "process.env.SENTRY_DSN")),
	def("node-gcpfunctions", "Google Cloud Functions (Node)", "node", "npm install @sentry/node",
		nodeSnippet("@sentry/node", "process.env.SENTRY_DSN")),
	def("node-hapi", "Hapi", "node", "npm install @sentry/node",
		nodeSnippet("@sentry/node", "process.env.SENTRY_DSN")),
	def("node-hono", "Hono", "node", "npm install @sentry/node",
		nodeSnippet("@sentry/node", "process.env.SENTRY_DSN")),
	def("node-koa", "Koa", "node", "npm install @sentry/node",
		nodeSnippet("@sentry/node", "process.env.SENTRY_DSN")),
	def("node-nestjs", "Nest.js", "node", "npm install @sentry/nestjs",
		nodeSnippet("@sentry/nestjs", "process.env.SENTRY_DSN")),
	def("objc", "Objective-C", "objc", "Add sentry-cocoa via CocoaPods or SPM.",
		genericSnippet("Objective-C", "sentry-cocoa")),
	def("other", "Other", "other", "Install whichever official Sentry SDK matches your stack.",
		func(dsn, env string) string {
			return fmt.Sprintf(`DSN: %s
Environment: %s

Any official Sentry SDK works: it talks the same envelope protocol
regardless of language. Point it at the DSN above.`, dsn, env)
		}),
	def("perl", "Perl", "perl", "cpan install Sentry::Raven",
		genericSnippet("Perl", "Sentry::Raven")),
	def("php", "PHP", "php", "composer require sentry/sentry",
		func(dsn, env string) string {
			return fmt.Sprintf(`\\Sentry\\init([
    'dsn' => %q,
    'environment' => %q,
]);`, dsn, env)
		}),
	def("php-laravel", "Laravel", "php", "composer require sentry/sentry-laravel",
		func(dsn, env string) string {
			return fmt.Sprintf(`# .env
SENTRY_LARAVEL_DSN=%s
SENTRY_ENVIRONMENT=%s`, dsn, env)
		}),
	def("php-symfony", "Symfony", "php", "composer require sentry/sentry-symfony",
		func(dsn, env string) string {
			return fmt.Sprintf(`# .env
SENTRY_DSN=%s
SENTRY_ENVIRONMENT=%s`, dsn, env)
		}),
	def("playstation", "PlayStation", "console", "Contact Sentry for the console SDK.",
		genericSnippet("PlayStation", "console SDK")),
	def("powershell", "PowerShell", "powershell", "Install-Module Sentry.PowerShell",
		genericSnippet("PowerShell", "Sentry.PowerShell")),
	def("python", "Python", "python", "pip install sentry-sdk",
		pythonSnippet("", "")),
	def("python-aiohttp", "AIOHTTP", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.aiohttp import AioHttpIntegration", "")),
	def("python-asgi", "ASGI", "python", "pip install sentry-sdk",
		pythonSnippet("# sentry_sdk auto-detects ASGI - call init() before creating the app.", "")),
	def("python-awslambda", "AWS Lambda (Python)", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.aws_lambda import AwsLambdaIntegration", "")),
	def("python-bottle", "Bottle", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.bottle import BottleIntegration", "")),
	def("python-celery", "Celery", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.celery import CeleryIntegration", "")),
	def("python-chalice", "Chalice", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.chalice import ChaliceIntegration", "")),
	def("python-django", "Django", "python", "pip install sentry-sdk",
		func(dsn, env string) string {
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
		}),
	def("python-falcon", "Falcon", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.falcon import FalconIntegration", "")),
	def("python-fastapi", "FastAPI", "python", "pip install sentry-sdk",
		func(dsn, env string) string {
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
		}),
	def("python-flask", "Flask", "python", "pip install sentry-sdk",
		func(dsn, env string) string {
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
		}),
	def("python-gcpfunctions", "Google Cloud Functions (Python)", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.gcp import GcpIntegration", "")),
	def("python-litestar", "Litestar", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.litestar import LitestarIntegration", "")),
	def("python-pylons", "Pylons", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.pylons import PylonsIntegration", "")),
	def("python-pymongo", "PyMongo", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.pymongo import PyMongoIntegration", "")),
	def("python-pyramid", "Pyramid", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.pyramid import PyramidIntegration", "")),
	def("python-quart", "Quart", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.quart import QuartIntegration", "")),
	def("python-rq", "RQ (Redis Queue)", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.rq import RqIntegration", "")),
	def("python-sanic", "Sanic", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.sanic import SanicIntegration", "")),
	def("python-serverless", "Serverless (Python)", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.serverless import serverless_function", "")),
	def("python-starlette", "Starlette", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.starlette import StarletteIntegration", "")),
	def("python-tornado", "Tornado", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.tornado import TornadoIntegration", "")),
	def("python-tryton", "Tryton", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.trytond import TrytondIntegration", "")),
	def("python-wsgi", "WSGI", "python", "pip install sentry-sdk",
		pythonSnippet("from sentry_sdk.integrations.wsgi import SentryWsgiMiddleware", "")),
	def("react-native", "React Native", "react-native", "npm install @sentry/react-native",
		func(dsn, env string) string {
			return fmt.Sprintf(`import * as Sentry from "@sentry/react-native";

Sentry.init({
  dsn: %q,
  environment: %q,
});`, dsn, env)
		}),
	def("ruby", "Ruby", "ruby", "gem install sentry-ruby",
		func(dsn, env string) string {
			return fmt.Sprintf(`require "sentry-ruby"

Sentry.init do |config|
  config.dsn = %q
  config.environment = %q
end`, dsn, env)
		}),
	def("ruby-rack", "Rack Middleware", "ruby", "gem install sentry-ruby",
		genericSnippet("Rack", "sentry-ruby")),
	def("ruby-rails", "Rails", "ruby", "gem install sentry-rails",
		func(dsn, env string) string {
			return fmt.Sprintf(`# config/initializers/sentry.rb
Sentry.init do |config|
  config.dsn = %q
  config.environment = %q
  config.breadcrumbs_logger = [:active_support_logger]
end`, dsn, env)
		}),
	def("rust", "Rust", "rust", "cargo add sentry",
		func(dsn, env string) string {
			return fmt.Sprintf(`let _guard = sentry::init((%q, sentry::ClientOptions {
    environment: Some(%q.into()),
    ..Default::default()
}));`, dsn, env)
		}),
	def("unity", "Unity", "unity", "Add the Sentry Unity SDK via the Unity Package Manager.",
		genericSnippet("Unity", "sentry-unity")),
	def("unreal", "Unreal Engine", "unreal", "Add the Sentry Unreal plugin.",
		genericSnippet("Unreal Engine", "sentry-unreal")),
	def("xbox", "Xbox", "console", "Contact Sentry for the console SDK.",
		genericSnippet("Xbox", "console SDK")),
}

var byID = func() map[string]Platform {
	m := make(map[string]Platform, len(All))
	for _, p := range All {
		m[p.ID] = p
	}
	return m
}()

// Aliases keeps pre-parity project rows working: the old picker used
// Sentry-incompatible shorthand IDs that now resolve to their canonical
// rustrak equivalents.
var Aliases = map[string]string{
	"nodejs":             "node",
	"nextjs":             "javascript-nextjs",
	"javascript-browser": "javascript",
	"java-other":         "java",
}

const DefaultID = "python-django"

// normalize resolves an alias to its canonical ID.
func normalize(id string) string {
	if target, ok := Aliases[id]; ok {
		return target
	}
	return id
}

// Get returns the platform for id, resolving aliases first and falling back
// to the default if id is empty or unrecognized (e.g. an older project row
// from before this field existed).
func Get(id string) Platform {
	id = normalize(id)
	if p, ok := byID[id]; ok {
		return p
	}
	return byID[DefaultID]
}

func Valid(id string) bool {
	id = normalize(id)
	_, ok := byID[id]
	return ok
}

// LanguageLabels mirrors rustrak's LANGUAGE_LABELS for grouping the picker.
var LanguageLabels = map[string]string{
	"as3": "ActionScript", "c": "C", "cfml": "ColdFusion", "cocoa": "Cocoa",
	"csharp": "C#", "groovy": "Groovy", "haskell": "Haskell", "objc": "Objective-C",
	"other": "Other", "perl": "Perl", "apple": "Apple", "android": "Android",
	"bun": "Bun", "capacitor": "Capacitor", "console": "Consoles", "cordova": "Cordova",
	"dart": "Dart", "deno": "Deno", "dotnet": ".NET", "electron": "Electron",
	"elixir": "Elixir", "flutter": "Flutter", "go": "Go", "godot": "Godot",
	"ionic": "Ionic", "java": "Java", "javascript": "JavaScript", "kotlin": "Kotlin",
	"minidump": "Minidump", "native": "Native", "node": "Node.js", "php": "PHP",
	"powershell": "PowerShell", "python": "Python", "react-native": "React Native",
	"ruby": "Ruby", "rust": "Rust", "unity": "Unity", "unreal": "Unreal Engine",
}
