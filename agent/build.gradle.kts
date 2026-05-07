import com.google.protobuf.gradle.id

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("com.google.protobuf")
}

val protobufVersion = "4.34.1"
val dropcheckVersion = providers.gradleProperty("dropcheckVersion").orElse("0.0.0-dev").get()

android {
    namespace = "io.dropcheck.agent"
    compileSdk {
        version = release(36) {
            minorApiLevel = 1
        }
    }
    buildToolsVersion = "36.1.0"

    defaultConfig {
        applicationId = "io.dropcheck.agent"
        minSdk = 31
        targetSdk = 36
        versionCode = 1
        versionName = dropcheckVersion
    }

    buildFeatures {
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlin {
        jvmToolchain(17)
    }
}

protobuf {
    protoc {
        artifact = "com.google.protobuf:protoc:$protobufVersion"
    }
    plugins {
        id("grpc") {
            artifact = "io.grpc:protoc-gen-grpc-java:1.81.0"
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
    implementation("io.grpc:grpc-okhttp:1.81.0")
    implementation("io.grpc:grpc-protobuf-lite:1.81.0")
    implementation("io.grpc:grpc-stub:1.81.0")
    implementation("com.google.protobuf:protobuf-javalite:$protobufVersion")
    compileOnly("javax.annotation:javax.annotation-api:1.3.2")
    testImplementation("junit:junit:4.13.2")
}
