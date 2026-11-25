# 近传

近传在同一局域网内连接一台电脑与一台手机。电脑端支持 Windows 10/11 x64，安卓端支持 Android 8.0 及以上。手机也可以用浏览器扫码连接。

[下载安装包](https://github.com/jwu469000-bot/near-transfer/releases/tag/v1.0.0)

## 开始使用

1. 让手机和电脑连上同一个 Wi‑Fi，双击运行 **NearTransfer-Windows-1.0.0.exe**。工作台会在电脑浏览器中打开。
2. 点击工作台右上角的 **安装安卓端**，用手机相机扫描安装二维码并下载 APK。也可直接安装 **NearTransfer-Android-1.0.0.apk**。
3. 打开安卓端近传，用 **打开相机** 扫描工作台上的配对二维码。也可以填写二维码下方的电脑地址与六位配对码。
4. 在任意一端选择文件。电脑端也支持拖放。接收方在 **传输记录** 中点 **下载**。安卓会让你选择保存位置。

更换设备时，在电脑端点 **更换连接**。正常结束程序会清理电脑上的中转文件，请先下载要保留的文件。

如果扫码后无法访问电脑，可检查 Windows 防火墙是否允许近传在当前网络上接收连接，以及 Wi‑Fi 是否开启设备隔离。电脑更换网络后，请重新打开近传以更新地址。文件通过当前局域网的 HTTP 连接传输。

## 从源码构建

### Windows

在 Windows x64 上安装 Go 1.24 或更新版本，从源码目录运行。

```powershell
go build -trimpath -ldflags "-s -w" -o NearTransfer-Windows-1.0.0.exe .
```

将已签名的 APK 放在 `web/install/near-transfer-android.apk` 后再构建，工作台即可提供安卓安装二维码。可使用对应版本发布页中的 APK。

### Android

准备 Python 3、JDK 17 或更新版本、Android SDK Platform 35、Build-Tools 35.0.0 及自己的 PKCS12 签名密钥。扫码依赖 ZXing Core 3.5.3，构建时从 Maven Central 下载依赖并校验 SHA‑256。

设置 `ANDROID_HOME`、`JAVA_HOME` 和签名密码环境变量 `NEAR_SIGN_PASSWORD`，然后运行。

```powershell
python android/build.py --keystore <你的签名文件.p12> --alias <密钥别名> --output NearTransfer-Android-1.0.0.apk
```

后续覆盖安装需使用与原安装包相同的签名证书。

## 许可证

本项目采用 [MIT 许可证](LICENSE)。第三方组件许可证随源码提供。
