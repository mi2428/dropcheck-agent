import com.google.protobuf.gradle.id

plugins {
    id("com.android.application")
    id("com.google.protobuf")
}

val protobufVersion = "4.36.2"
val grpcVersion = "1.85.1"
val dropcheckVersion = providers.gradleProperty("dropcheckVersion").orElse("0.0.0-dev").get()

android {
    namespace = "io.dropcheck.agent"
    compileSdk {
        version = release(37) { minorApiLevel = 2 }
    }
    buildToolsVersion = "37.0.0"

    defaultConfig {
        applicationId = "io.dropcheck.agent"
        minSdk = 31
        targetSdk = 37
        versionCode = 1
        versionName = dropcheckVersion
    }

    buildFeatures {
        buildConfig = true
    }

    lint {
        warningsAsErrors = true
    }

    // Use the controller's canonical device-free command matrix for Shell parity.
    sourceSets.getByName("test").resources.directories.add("../controller/integration/e2e/testdata")
    sourceSets.getByName("test").resources.directories.add("../shell")
    sourceSets.getByName("main").resources.directories.add("../controller/internal/oui/data")

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin {
    jvmToolchain(17)
}

protobuf {
    protoc {
        artifact = "com.google.protobuf:protoc:$protobufVersion"
    }
    plugins {
        id("grpc") {
            artifact = "io.grpc:protoc-gen-grpc-java:$grpcVersion"
        }
    }
    generateProtoTasks {
        all().forEach { task ->
            task.builtins {
                id("java") {
                    option("lite")
                }
            }
            task.plugins {
                id("grpc") {
                    option("lite")
                }
            }
        }
    }
}

dependencies {
    implementation("com.google.re2j:re2j:1.8")
    implementation("io.grpc:grpc-okhttp:$grpcVersion")
    implementation("io.grpc:grpc-protobuf-lite:$grpcVersion")
    implementation("io.grpc:grpc-stub:$grpcVersion")
    implementation("com.google.protobuf:protobuf-javalite:$protobufVersion")
    testImplementation("junit:junit:4.13.2")
}
