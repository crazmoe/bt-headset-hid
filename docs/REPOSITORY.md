# Package repository

`.github/workflows/repo.yml` builds an apt and a dnf repository from the deb and rpm packages of
all releases and publishes it on GitHub Pages at <https://crazmoe.github.io/bt-headset-hid/>. It
runs after each release and by hand (Actions, "Package repository", "Run workflow").

Once `packaging/repo-key.asc` exists, the packages built by `release.yml` add the repository
themselves (`/etc/apt/sources.list.d/bt-headset-hid.sources` or
`/etc/yum.repos.d/bt-headset-hid.repo`, key in `/usr/share/keyrings/bt-headset-hid.asc`), so
whoever installs a package gets later versions with the system updates. Without the file the
packages are built as before.

## Setting it up (once)

1. Create a signing key without a passphrase, on any computer with GnuPG:

   ```
   export GNUPGHOME=$(mktemp -d)
   gpg --batch --passphrase '' --quick-gen-key "bt-headset-hid package repository" rsa4096 sign never
   gpg --armor --export-secret-keys > repo-signing-key.asc
   gpg --armor --export > repo-key.asc
   ```

2. In the repository settings, under Secrets and variables, Actions, add the secret
   `REPO_SIGNING_KEY` with the contents of `repo-signing-key.asc`. Then delete that file and the
   `GNUPGHOME` folder; a backup of the key in a password manager lets you replace the secret later.

3. Commit `repo-key.asc` as `packaging/repo-key.asc`.

4. In the repository settings, under Pages, choose "GitHub Actions" as the source.

5. Run the workflow "Package repository" once by hand. From the next release on, the packages
   add the repository.

## Adding it by hand

For packages built before the key existed:

```
# Debian, Ubuntu
sudo curl -fsSLo /usr/share/keyrings/bt-headset-hid.asc https://crazmoe.github.io/bt-headset-hid/key.asc
printf 'Types: deb\nURIs: https://crazmoe.github.io/bt-headset-hid/apt/\nSuites: ./\nSigned-By: /usr/share/keyrings/bt-headset-hid.asc\n' |
  sudo tee /etc/apt/sources.list.d/bt-headset-hid.sources
sudo apt update && sudo apt install bt-headset-hid

# Fedora
sudo curl -fsSLo /etc/yum.repos.d/bt-headset-hid.repo https://crazmoe.github.io/bt-headset-hid/rpm/bt-headset-hid.repo
sudo dnf install bt-headset-hid
```

The repository metadata is signed (apt: `InRelease`, dnf: `repomd.xml.asc`); the rpm packages
themselves are not, so the `.repo` file sets `gpgcheck=0` and `repo_gpgcheck=1`.
