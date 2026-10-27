# Android

A reusable Dagger module for building, linting, testing, and signing Android
Gradle projects. It runs the project's Gradle wrapper in an Android SDK container
and supports product flavors, multi-module projects, prepared source trees, and
custom toolchains.

Requires Dagger **v1.0.0-beta.20 or later**. No host JDK, Gradle, or Android SDK is
needed. The module builds artifacts; it does not upload them to an app store.

## Install

```sh
dagger module install github.com/dagger/android
```

Configure the module in your project's `dagger.toml`:

```toml
[modules.android]
source = "github.com/dagger/android"

[modules.android.settings]
source = "."
compileSdk = 36
```

The defaults expect `gradlew` at the source root and an application module named
`app` in `app/`. Set `source` to the directory containing your Android project, or
use `projectPath` when Gradle needs access to other directories in the repository.

```sh
# Assemble, lint, and run JVM unit tests for the configured checkVariant.
dagger check --module android

# Run one check.
dagger check --module android --check lint

# Export APKs or Android App Bundles.
dagger api call android assemble export --path ./dist/apk
dagger api call android bundle export --path ./dist/bundle

# Select a flavored variant.
dagger api call android assemble --variant demoDebug export --path ./dist/apk

# Run arbitrary tasks and export their reports.
dagger api call android run --tasks :app:lintRelease \
  directory --path app/build/reports export --path ./dist/reports
```

`assemble`, `bundle`, and `signed-bundle` return a **Directory**, preserving
Gradle's filenames, variant subdirectories, split APKs, and output metadata.
For example, the default debug APK is usually `debug/app-debug.apk` inside the
returned directory. Chain `file --path ...` when you need one specific artifact.
Signing for `assemble` and `bundle` follows your Gradle configuration.

## Test collection

`tests` is a Dagger collection keyed by fully qualified JVM test class name for
the configured `gradleModule` and `checkVariant`:

```sh
dagger list android-test-classes -a

# Run all methods and parameterized cases in one class.
dagger check --android-test-class com.example.ArithmeticTest

# Run a selection together in one Gradle invocation.
dagger check \
  --android-test-class com.example.ArithmeticTest \
  --android-test-class com.example.OtherTest

# Call the collection API directly.
dagger api call android tests keys
dagger api call android tests get --key com.example.ArithmeticTest test
```

Discovery uses [Gradle's test dry-run mode](https://docs.gradle.org/current/userguide/java_testing.html#sec:test_execution),
so it honors Gradle's source sets and framework configuration, including inherited
and parameterized tests. It requires **Gradle 8.3 or later**. Listing classes
compiles the tests and loads the test framework; framework discovery code such as
parameter providers may run, but test bodies do not. Selected runs reuse the
compiled container and execute the tests with dry-run mode disabled.

The collection's `test` check batches selected classes. With no selection it runs
the whole suite using Gradle's configuration; an empty selection runs nothing.
`dagger check --module android --check test` uses this collection. The direct
`dagger api call android test` function still runs the suite without discovery,
including on older Gradle versions. It is no longer a separate check, so selecting
a class does not also trigger an unfiltered suite run. Device and emulator tests
are outside this collection.

## Configuration

| Setting | Default | Purpose |
| --- | --- | --- |
| `source` | Unset | Directory containing the build context. Required unless `sourceCtr` is set. |
| `sourceCtr` | Unset | Prepared source container; takes precedence over `source`. |
| `sourcePath` | `.` | Directory inside `sourceCtr`; defaults to that container's working directory. |
| `projectPath` | `.` | Gradle root relative to the source context. |
| `gradleModule` | `app` | Gradle selector, such as `:apps:client`; empty selects the root project. |
| `modulePath` | `app` | Module's filesystem directory relative to the Gradle root. |
| `buildDirectory` | Empty | Build directory relative to the Gradle root; defaults to `<modulePath>/build`. |
| `checkVariant` | `debug` | Variant for the `build`, `lint`, and `test` checks. |
| `compileSdk` | `36` | API level used to select the default SDK image. Does not change Gradle configuration. |
| `baseImageAddress` | Empty | Override `ghcr.io/cirruslabs/android-sdk:<compileSdk>`. |
| `baseCtr` | Unset | Custom SDK container, overriding the image and platform settings. |
| `platform` | `linux/amd64` | Platform used when creating the SDK container. |
| `gradleCommand` | `["sh", "./gradlew"]` | Command prefix; invoking the wrapper through `sh` does not require its executable bit. |
| `gradleArguments` | `[]` | Arguments added to every invocation, such as `--stacktrace` or `-Pname=value`. |
| `cacheKey` | `android-gradle` | Namespace for persistent Gradle dependency and wrapper caches. |
| `requiredApkPaths` | `[]` | Exact archive entries required in every APK built by the `build` check. |
| `sourceExcludes` | `["**/.gradle", "**/build", "**/local.properties"]` | Exclude host Gradle state, stale build outputs, and host SDK paths. |

The image must provide a compatible JDK and Android SDK with accepted licenses.
The default is the [Cirrus Labs SDK image](https://github.com/cirruslabs/docker-images-android).
Use an image digest or your workspace's `dagger.lock` to pin the toolchain.
`linux/amd64` supports Google's Linux Android build tools and runs under emulation
on ARM hosts. Choose another platform only with a toolchain that supports it.

`base` returns the configured container for further composition. `baseCtr` can
supply extra SDK packages, an alternate JDK, repository credentials via Dagger
secrets, or service bindings. `sourceCtr` supplies files only; its environment,
secrets, and services are not inherited by the build container.

Gradle's `caches` and `wrapper` directories are mounted under `/gradle`, with
`GRADLE_USER_HOME=/gradle`. Locked cache mounts prevent concurrent Gradle processes
from contending for the same cache. Builds sharing a cache namespace serialize;
use different `cacheKey` values when isolation is needed.

## Nested projects and prepared sources

The Gradle project selector and its filesystem path are independent:

```toml
[modules.android.settings]
source = "."
projectPath = "mobile/android"
gradleModule = ":apps:client"
modulePath = "applications/client"
checkVariant = "demoDebug"
requiredApkPaths = ["assets/index.html"]
```

The entire context is available at `/project`; Gradle runs from
`/project/mobile/android`. This preserves sibling dependencies needed by projects
such as Capacitor or React Native. An upstream module can prepare that context:

```toml
[modules.android.settings]
sourceCtr = "frontend:android-source"
sourcePath = "/prepared"
projectPath = "android"
```

Here `frontend:android-source` is a Container-returning function in your workspace.
See [Dagger module wiring](https://docs.dagger.io/config/module-wiring/).
If prepared dependencies contain necessary `build/` directories, override
`sourceExcludes` to preserve those directories while still excluding stale Android
outputs, for example `["**/.gradle", "**/local.properties", "android/app/build"]`.
The selected module's build directory is always removed from the input context,
including when `buildDirectory` points to a custom location.

For a custom Gradle build directory, set `buildDirectory`. For a different artifact
layout, pass `--output-path` to `assemble`, `bundle`, or `signed-bundle`; it is
relative to the Gradle root. An Android library can use `assemble --output-path
library/build/outputs/aar`. The generic `run` function supports any Gradle task,
including tasks across multiple modules. Device tests require an emulator or
device setup supplied by the caller; the `test` check runs JVM unit tests only.

`requiredApkPaths` checks every produced APK using literal entry names, not regular
expressions or shell patterns. Leave it empty for library builds or split APKs
whose contents intentionally differ.

## Signing

`signed-bundle` builds a selected variant, verifies every returned AAB against the
supplied keystore and alias, and rejects unsigned entries or invalid signatures.
It passes `--rerun-tasks --no-build-cache` to Gradle so a previous signed artifact
cannot bypass the supplied credentials. Dagger can still cache identical calls.

Your Gradle project must read these environment variables:

```groovy
android {
    signingConfigs {
        release {
            if (System.getenv('ANDROID_KEYSTORE_FILE')) {
                storeFile file(System.getenv('ANDROID_KEYSTORE_FILE'))
                storePassword System.getenv('ANDROID_KEYSTORE_PASSWORD')
                keyAlias System.getenv('ANDROID_KEY_ALIAS')
                keyPassword System.getenv('ANDROID_KEY_PASSWORD')
            }
        }
    }
    buildTypes {
        release {
            if (System.getenv('ANDROID_KEYSTORE_FILE')) {
                signingConfig signingConfigs.release
            }
        }
    }
}
```

With the password environment variables set on the host:

```sh
dagger api call android signed-bundle \
  --keystore file://./upload-keystore.jks \
  --keystore-password env://KEYSTORE_PASSWORD \
  --key-alias upload \
  --key-password env://KEY_PASSWORD \
  --variant release \
  export --path ./dist/signed-bundle
```

The keystore and passwords are Dagger secrets. Do not pass credentials through
`gradleArguments`; use secret environment variables or mounts on `baseCtr` for
other credential conventions. Signing is an explicit function, not an unattended
check.

## Develop this module

The Dang SDK manages the root module and its client in `.dagger/modules/e2e/`.
Test fixtures live in that module's `testdata/` directory.

```sh
dagger generate -y
dagger check
```

Tests cover argument boundaries, nested source contexts, custom commands, flavored
variants, multiple APKs, exact archive checks, and failures. Independent fixtures
exercise Android/JUnit 4 and JUnit 5 collection discovery, individual and batched
selection, parameterized and inherited tests, dynamic cases, and empty suites.
A deliberately failing JUnit 5 fixture verifies that discovery skips test bodies
and selected runs execute them. The Android fixture also builds an APK and an AAB,
runs Lint, and executes JVM tests with the actual SDK. The first run downloads the
SDK image and Gradle dependencies.

Try collection commands against the included Android fixture:

```sh
dagger --env fixture list android-test-classes -a
dagger --env fixture check --android-test-class com.example.fixture.ParameterizedTest
```

Run the signing tests with a disposable key:

```sh
dagger -m .dagger/modules/e2e api call test-keystore export --path /tmp/android-module-test.p12
dagger -m .dagger/modules/e2e api call signing --keystore file:///tmp/android-module-test.p12
rm /tmp/android-module-test.p12
```

The signing test checks a valid signed AAB, invalid credentials, and rejection of an
unsigned AAB. The fixture contains no production keys or application configuration.

To call the local module directly:

```sh
dagger -m . api call --source .dagger/modules/e2e/testdata/android \
  --gradle-module client --module-path apps/client \
  assemble --variant demoDebug export --path /tmp/android-apk
```

The fixture wrapper can be regenerated with:

```sh
dagger -m .dagger/modules/e2e api call wrapper export \
  --path .dagger/modules/e2e/testdata/android
```

Generated wrapper files retain Gradle's license notices.
