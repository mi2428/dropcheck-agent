import com.google.protobuf.gradle.id

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("com.google.protobuf")
}

val protobufVersion = "4.34.1"

android {
    namespace = "io.dropcheck.agent"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.dropcheck.agent"
        minSdk = 31
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
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
