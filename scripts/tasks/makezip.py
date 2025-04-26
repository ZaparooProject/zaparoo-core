import os
import shutil
import sys
import urllib.request
import zipfile

# IMPORTANT: This script must be run from the root of the repo!

base_url: str = "https://github.com/ZaparooProject/zaparoo.org/raw/refs/heads/main/docs/platforms/"
platform_docs: dict[str, str] = {
    "batocera": "batocera.md",
    "bazzite": "bazzite.mdx",
    "chimeraos": "chimeraos.mdx",
    "libreelec": "libreelec.mdx",
    "linux": "linux.mdx",
    "mac": "mac.mdx",
    "mister": "mister.md",
    "mistex": "mistex.md",
    "recalbox": "recalbox.mdx",
    "steamos": "steamos.md",
    "windows": "windows/index.md"
}
extra_files: dict[str, list[str]] = {
    "batocera": ["cmd/batocera/scripts"]
}


def strip_frontmatter(content: str) -> str:
    lines = content.splitlines()
    if lines[0] == "---":
        for i in range(1, len(lines)):
            if lines[i] == "---":
                return "\n".join(lines[i + 1:])
    return content


def download_doc(platform_id: str, to_dir: str):
    if platform_id not in platform_docs:
        raise ValueError(f"Platform '{platform_id}' not found in the platforms list.")

    file_name = platform_docs[platform_id]
    url = os.path.join(base_url, file_name)

    with urllib.request.urlopen(url) as response:
        content = response.read().decode("utf-8")

    if file_name.lower().endswith(".mdx"):
        content = strip_frontmatter(content)

    with open(os.path.join(to_dir, "README.txt"), "w", encoding="utf-8") as file:
        file.write(content.strip() + "\n")


if __name__ == "__main__":
    if len(sys.argv) < 5:
        raise ValueError("Usage: makezip.py <platform> <build_dir> <app_bin> <zip_name>")

    platform = sys.argv[1]
    build_dir = sys.argv[2]
    app_bin = sys.argv[3]
    zip_name = sys.argv[4]

    if not os.path.isdir(build_dir):
        raise NotADirectoryError(f"The specified directory '{build_dir}' does not exist.")

    license_path = os.path.join(build_dir, "LICENSE.txt")
    if not os.path.isfile(license_path):
        shutil.copy("LICENSE", license_path)

    app_path = os.path.join(build_dir, app_bin)
    if not os.path.isfile(app_path):
        raise FileNotFoundError(f"The specified binary file '{app_path}' does not exist.")

    zip_path = os.path.join(build_dir, zip_name)
    if os.path.isfile(zip_path):
        os.remove(zip_path)

    readme_path = os.path.join(build_dir, "README.txt")
    if os.path.exists(readme_path):
        print(f"File '{readme_path}' already exists. Skipping download.")
    else:
        download_doc(platform, build_dir)

    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as dist:
        dist.write(app_path, arcname=os.path.basename(app_path))
        dist.write(license_path, arcname=os.path.basename(license_path))
        dist.write(readme_path, arcname=os.path.basename(readme_path))

        if platform in extra_files:
            for file in extra_files[platform]:
                if os.path.isfile(file):
                    shutil.copy(file, build_dir)
                if os.path.isdir(file):
                    shutil.copytree(file, build_dir, dirs_exist_ok=True)
                dist.write(file, arcname=os.path.basename(file))
