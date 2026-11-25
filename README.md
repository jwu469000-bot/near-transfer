# 近传 NearTransfer

近传让一台 Windows 电脑和一台安卓手机在局域网内互传文件和文字。首次配对后，两端打开应用即可自动连接。Windows 端平时显示为一个可拖动的小球。电脑端支持 Windows 10/11 x64，安卓端支持 Android 8.0 及以上。

## 开始使用

1. 按下方说明构建两端程序。让两台设备连上能互相访问的同一个 Wi‑Fi。双击 **NearTransfer-Windows-1.1.1.exe**，桌面会出现小球。单击展开互传面板。
2. 在面板里点 **扫码配对手机**，或从右上角菜单选 **配对与安装**。这会打开单独的 Windows 窗口。
3. 首次安装时，进入 **安装安卓端**。可用手机相机扫码下载，也可以点 **保存 APK**，将 **NearTransfer-Android-1.1.1.apk** 保存到电脑后传给手机。
4. 在手机近传里扫 **设备配对** 页的二维码，也可以填写电脑地址和六位配对码。绑定后，两端打开近传会自动重连。

## 收发文件

1. 从电脑发送时，把文件拖到小球或展开的面板，或在面板里点发送区多选，随后在手机上下载保存。
2. 从手机发送时，用 App 的系统选择器选择文件。送达后文件自动存进电脑的 **下载\近传**，可由小摆件底部的 **收件箱** 打开。遇到同名文件会另存一份。

按住小球或展开面板的标题区可移动位置，小球和面板都会限制在屏幕可用区域内。点击面板右上角的 **−** 收起。菜单可以切换置顶、选择登录 Windows 时打开、解除绑定或退出近传。

## 互传文字

在任一端点击 **传文字**，输入或粘贴后点击 **发送文字**。另一端打开 **传文字**，可查看本次连接的文字并点 **复制文字**。收起的小球会用小圆点提示收到新内容。

安卓 1.1.1 可以覆盖安装到已绑定的 1.1.0 上。两端都使用 1.1.1 即可互传文字。单条文字最多 256 KB，记录随电脑上的本次连接保存。

## 连接与保存

已绑定的手机会优先尝试上次连通的电脑地址，并在需要时尝试在局域网内发现电脑。网络限制发现时，安卓 App 的 **连接设置 → 更新电脑地址** 可以手动填入小摆件设置页上的新地址，继续使用当前绑定。

无法访问时，请检查 Windows 防火墙是否允许近传接收连接，以及 Wi‑Fi 是否开启设备隔离。文件和文字通过当前局域网的 HTTP 连接传输，路由器断开外网时也可使用。请在信任的局域网使用。设备绑定和电脑收件箱会保留，电脑发出的待下载文件需在退出近传前保存。

## 从源码构建

### Windows

在 Windows x64 上安装 Go 1.24 或更新版本，从源码目录运行。

```powershell
go build -trimpath -ldflags "-H windowsgui -s -w" -o NearTransfer-Windows-1.1.1.exe .
```

要让原生配对窗口提供安卓安装二维码与本机保存，请先构建已签名的 APK，复制到 `web/install/near-transfer-android.apk`，再构建 Windows 端。

如需为 Windows 可执行文件添加近传图标，可先生成图标资源。

```powershell
go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -ico near-transfer.ico -o rsrc_windows_amd64.syso
```

### Android

准备 Python 3、JDK 17 或更新版本、Android SDK Platform 35、Build-Tools 35.0.0 及自己的 PKCS12 签名密钥。构建脚本会从 Maven Central 获取 ZXing Core 3.5.3 并核对 SHA‑256。离线构建时，可预先将相同依赖放入 `android/vendor/zxing-core-3.5.3.jar`。

设置 `ANDROID_HOME`、`JAVA_HOME` 和签名密码环境变量 `NEAR_SIGN_PASSWORD`，然后运行。

```powershell
python android/build.py --keystore .\near-transfer.p12 --alias near-transfer --output NearTransfer-Android-1.1.1.apk
```

后续覆盖安装需使用与原安装包相同的签名证书。

## 许可证

近传源码采用 [MIT 许可证](LICENSE)。[go-qrcode 的许可证](web/assets/go-qrcode-LICENSE.txt)、[ZXing 的许可证](android/vendor/ZXing-LICENSE.txt)和 [Go 运行时的许可证](web/assets/Go-LICENSE.txt)随项目提供。
