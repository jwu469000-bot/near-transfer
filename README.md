# 近传 1.1

近传给固定的一台 Windows 电脑和一台安卓手机互传文件。电脑端支持 Windows 10/11 x64，安卓端支持 Android 8.0 及以上。

[下载安装包](https://github.com/jwu469000-bot/near-transfer/releases/tag/v1.1.0)

## 开始使用

1. 让两台设备连上同一个 Wi‑Fi。双击 **NearTransfer-Windows-1.1.0.exe**，桌面右下角会出现小摆件。
2. 点小摆件里的 **扫码配对手机** 打开设置页。可点 **安装安卓端**，用手机相机扫码下载 APK，也可直接安装 **NearTransfer-Android-1.1.0.apk**。
3. 在安卓 App 里扫设置页上的配对二维码。也可以填页面显示的电脑地址和六位配对码。首次绑定完成后，每次打开两端近传就会自动重连。

1. 从电脑发送时，把文件拖进小摆件，或点击中间的发送区多选。随后在手机上下载保存。
2. 从手机发送时，用 App 的系统选择器选择文件。送达后文件自动存进电脑的 **下载\近传**，可由小摆件底部的 **收件箱** 打开。遇到同名文件会另存一份。

拖动小摆件的标题区可改变位置。右上角的菜单可以切换置顶、选择登录 Windows 时打开、解除绑定或退出近传。升级自 1.0.0 时，请用两个 1.1.0 客户端完成首次绑定。

已绑定的手机会优先尝试上次连通的电脑地址，并在需要时尝试在局域网内发现电脑。网络限制发现时，安卓 App 的 **连接设置 → 更新电脑地址** 可以手动填入小摆件设置页上的新地址，继续使用当前绑定。

无法访问时，请检查 Windows 防火墙是否允许近传接收连接，以及 Wi‑Fi 是否开启设备隔离。文件通过当前局域网的 HTTP 连接传输。设备绑定和电脑收件箱会保留，电脑发出的待下载文件需在退出近传前保存。

## 从源码构建

### Windows

在 Windows x64 上安装 Go 1.24 或更新版本，从源码目录运行。

```powershell
go build -trimpath -ldflags "-H windowsgui -s -w" -o NearTransfer-Windows-1.1.0.exe .
```

将已签名的 APK 放在 `web/install/near-transfer-android.apk` 后再构建，工作台即可提供安卓安装二维码。可使用对应版本发布页中的 APK。

### Android

准备 Python 3、JDK 17 或更新版本、Android SDK Platform 35、Build-Tools 35.0.0 及自己的 PKCS12 签名密钥。扫码依赖 ZXing Core 3.5.3，构建时从 Maven Central 下载依赖并校验 SHA‑256。

设置 `ANDROID_HOME`、`JAVA_HOME` 和签名密码环境变量 `NEAR_SIGN_PASSWORD`，然后运行。

```powershell
python android/build.py --keystore .\near-transfer.p12 --alias near-transfer --output NearTransfer-Android-1.1.0.apk
```

后续覆盖安装需使用与原安装包相同的签名证书。

## 许可证

本项目采用 [MIT 许可证](LICENSE)。第三方组件许可证随源码提供。
