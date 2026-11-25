"""Build and sign NearTransfer with the Android SDK and a JDK."""

from argparse import ArgumentParser
from hashlib import sha256
from pathlib import Path
from tempfile import TemporaryDirectory
from urllib.request import urlopen
from zipfile import ZipFile, ZIP_DEFLATED
import os
import shutil
import subprocess

ZXING_FILENAME = "zxing-core-3.5.3.jar"
ZXING_URL = "https://repo.maven.apache.org/maven2/com/google/zxing/core/3.5.3/core-3.5.3.jar"
ZXING_SHA256 = "8d8064c1636fdaef7189dd9055c7d59950a8940a12f2293956446ec3c109fd82"


def run(*command):
    subprocess.run([str(part) for part in command], check=True)


def zxing_dependency(vendor, work):
    local = vendor / ZXING_FILENAME
    if local.is_file():
        data = local.read_bytes()
    else:
        with urlopen(ZXING_URL, timeout=30) as response:
            data = response.read(2 * 1024 * 1024 + 1)
    if sha256(data).hexdigest() != ZXING_SHA256:
        raise ValueError(f"{ZXING_FILENAME} failed SHA-256 verification")
    dependency = work / ZXING_FILENAME
    dependency.write_bytes(data)
    return dependency


def build(args):
    root = Path(__file__).resolve().parent
    sdk = Path(args.sdk).resolve()
    build_tools = sdk / "build-tools" / "35.0.0"
    platform = sdk / "platforms" / "android-35" / "android.jar"
    vendor = root / "vendor"
    extension = ".exe" if os.name == "nt" else ""
    aapt = build_tools / ("aapt2" + extension)
    zipalign = build_tools / ("zipalign" + extension)
    java_home = os.environ.get("JAVA_HOME")
    java_bin = Path(java_home) / "bin" if java_home else None
    java = java_bin / ("java" + extension) if java_bin else "java"
    javac = java_bin / ("javac" + extension) if java_bin else "javac"
    d8 = Path(args.d8).resolve() if args.d8 else build_tools / "lib" / "d8.jar"
    signer = build_tools / "lib" / "apksigner.jar"
    output = Path(args.output).resolve()
    work_dir = Path(args.scratch).resolve() if args.scratch else None
    if work_dir:
        work_dir.mkdir(parents=True, exist_ok=True)

    with TemporaryDirectory(prefix="near-android-", dir=work_dir) as tmp:
        work = Path(tmp)
        zxing = zxing_dependency(vendor, work)
        generated = work / "generated"
        classes = work / "classes"
        dex = work / "dex"
        for folder in (generated, classes, dex):
            folder.mkdir()
        resources = work / "resources.zip"
        apk = work / "unpacked.apk"
        aligned = work / "aligned.apk"

        run(aapt, "compile", "--dir", root / "res", "-o", resources)
        run(aapt, "link", "-I", platform, "--manifest", root / "AndroidManifest.xml",
            "--java", generated, "-o", apk, resources)
        sources = sorted((root / "src").rglob("*.java")) + sorted(generated.rglob("*.java"))
        run(javac, "--release", "8", "-encoding", "UTF-8", "-classpath",
            os.pathsep.join([str(platform), str(zxing)]), "-d", classes, *sources)

        archive = work / "classes.jar"
        with ZipFile(archive, "w", ZIP_DEFLATED) as jar:
            for entry in sorted(classes.rglob("*.class")):
                jar.write(entry, entry.relative_to(classes).as_posix())
        run(java, "-cp", d8, "com.android.tools.r8.D8", "--min-api", "26", "--lib",
            platform, "--output", dex, archive, zxing)
        with ZipFile(apk, "a", ZIP_DEFLATED) as package:
            for entry in sorted(dex.glob("classes*.dex")):
                package.write(entry, entry.name)
            package.write(vendor / "ZXing-LICENSE.txt", "assets/licenses/ZXing-LICENSE.txt")
        run(zipalign, "-f", "4", apk, aligned)
        signed = work / "signed.apk"
        run(java, "-jar", signer, "sign", "--ks", Path(args.keystore).resolve(), "--ks-key-alias", args.alias,
            "--ks-pass", "env:" + args.password_env, "--key-pass", "env:" + args.password_env,
            "--v1-signing-enabled", "false", "--out", signed, aligned)
        run(java, "-jar", signer, "verify", "--verbose", signed)
        output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(signed, output)


if __name__ == "__main__":
    parser = ArgumentParser(description=__doc__)
    parser.add_argument("--sdk", default=os.environ.get("ANDROID_HOME"))
    parser.add_argument("--keystore", required=True)
    parser.add_argument("--alias", default="near-transfer")
    parser.add_argument("--password-env", default="NEAR_SIGN_PASSWORD")
    parser.add_argument("--output", required=True)
    parser.add_argument("--scratch")
    parser.add_argument("--d8", help="Use a separate, newer R8/D8 JAR if necessary")
    options = parser.parse_args()
    if not options.sdk:
        parser.error("Set ANDROID_HOME or pass --sdk.")
    if options.password_env not in os.environ:
        parser.error("Set the signing password environment variable.")
    build(options)
