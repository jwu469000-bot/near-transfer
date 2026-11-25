package cn.jinchuan.transfer;

import com.google.zxing.BinaryBitmap;
import com.google.zxing.PlanarYUVLuminanceSource;
import com.google.zxing.ReaderException;
import com.google.zxing.common.HybridBinarizer;
import com.google.zxing.qrcode.QRCodeReader;

final class QrDecoder {
    private final QRCodeReader reader = new QRCodeReader();

    String read(byte[] bytes, int width, int height) {
        PlanarYUVLuminanceSource luminance = new PlanarYUVLuminanceSource(
                bytes, width, height, 0, 0, width, height, false);
        try {
            return reader.decode(new BinaryBitmap(new HybridBinarizer(luminance))).getText();
        } catch (ReaderException notFound) {
            return null;
        } finally {
            reader.reset();
        }
    }
}
