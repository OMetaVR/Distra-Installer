# Distra Installer

The installer for [Distra](https://github.com/OMetaVR/Distra-Desktop), a Discord desktop client mod. It's a fork of
[Equilotl](https://github.com/Equicord/Equilotl), the Equicord installer, which is itself based on the Vencord installer.
This is a personal project, so please don't take issues with it to the Equicord or Vencord teams.

Builds are unsigned, so Windows SmartScreen and macOS Gatekeeper will ask you to allow them the first time.

![image](https://i.imgur.com/oHN41ss.png)

## Usage

Windows

| | X64 |
| --- | --- |
| GUI | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstaller.exe) |
| CLI | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstallerCli.exe) |

MacOS

| | X64 | ARM64 |
| --- | --- | --- |
| GUI | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstaller-x64.dmg) | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstaller-arm64.dmg) |
| CLI | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstallerCli-darwin-amd64) | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstallerCli-darwin-arm64) |

The CLI builds are plain binaries, so run `chmod +x <file>` after downloading.

Linux

| | X64 | ARM64 |
| --- | --- | --- |
| Combined GUI | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstaller) | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstaller-arm64) |
| X11 GUI | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstaller-x11) | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstaller-x11-arm64) |
| Wayland GUI | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstaller-wayland) | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstaller-wayland-arm64) |
| CLI | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstallerCli-linux) | [Download](https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstallerCli-linux-arm64) |

Or on Linux, in a terminal:

```sh
sh -c "$(curl -sS https://raw.githubusercontent.com/OMetaVR/Distra-Installer/main/install.sh)"
```

## Building from source

### Prerequisites

You need to install the [Go programming language](https://go.dev/doc/install) and GCC, the GNU Compiler Collection (MinGW on Windows)

<details>
<summary>Additionally, if you're using Linux, you have to install some additional dependencies:</summary>

#### Base dependencies

```sh
apt install -y pkg-config libsdl2-dev libglx-dev libgl1-mesa-dev
dnf install pkg-config libGL-devel libXxf86vm-devel
```

#### X11 dependencies

```sh
apt install -y xorg-dev
dnf install libXcursor-devel libXi-devel libXinerama-devel libXrandr-devel
```

#### Wayland dependencies

```sh
apt install -y libwayland-dev libxkbcommon-dev wayland-protocols extra-cmake-modules
dnf install wayland-devel libxkbcommon-devel wayland-protocols-devel extra-cmake-modules
```

</details>

### Building

#### Install dependencies

```sh
go mod tidy
```

#### Build the GUI

##### Windows / Mac / Linux X11

```sh
make GUI=1
```

##### Linux Wayland

```sh
make GUI=1 WAYLAND=1
```

#### Build the CLI

```
go build --tags cli
```

You might want to pass some flags to this command to get a better build.
See [the GitHub workflow](https://github.com/OMetaVR/Distra-Installer/blob/main/.github/workflows/distra-release.yml) for what flags I pass or if you want more precise instructions
