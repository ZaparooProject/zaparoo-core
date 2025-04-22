import os
import sys
import urllib.request

base_url: str = "https://github.com/ZaparooProject/zaparoo.org/raw/refs/heads/main/docs/platforms/"
platform_docs: dict[str, str] = {
    "batocera": "batocera.mdx",
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


def strip_frontmatter(content: str) -> str:
    lines = content.splitlines()
    if lines[0] == "---":
        for i in range(1, len(lines)):
            if lines[i] == "---":
                return "\n".join(lines[i + 1:])
    return content


def download_doc(platform_id: str):
    if platform_id not in platform_docs:
        raise ValueError(f"Platform '{platform_id}' not found in the platforms list.")

    file_name = platform_docs[platform_id]
    url = os.path.join(base_url, file_name)

    with urllib.request.urlopen(url) as response:
        content = response.read().decode("utf-8")

    if file_name.lower().endswith(".mdx"):
        content = strip_frontmatter(content)

    with open("README.txt", "w", encoding="utf-8") as file:
        file.write(content.strip() + "\n")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        raise ValueError("Provide platform as argument.")

    platform = sys.argv[1]
    target_dir = sys.argv[2] if len(sys.argv) > 2 else "."

    if not os.path.isdir(target_dir):
        raise NotADirectoryError(f"The specified directory '{target_dir}' does not exist.")

    os.chdir(target_dir)

    readme_path = os.path.join(target_dir, "README.txt")
    if os.path.exists(readme_path):
        print(f"File '{readme_path}' already exists. Skipping download.")
    else:
        download_doc(platform)
