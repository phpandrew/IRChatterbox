<?php
// IRChatterbox image host: one file, no dependencies beyond PHP's GD extension.
//
// POST https://your-server/upload.php with a multipart "image" field → JSON:
//   200 {"ok":true,"url":"https://your-server/i/7/k3/k3x9Qa2mZp.webp","width":1000,"height":750,"bytes":38211,"expires":"2026-10-24T18:00:00Z"}
//   4xx/5xx {"ok":false,"error":"Only JPG, PNG, WebP and BMP images can be uploaded."}
//
// Each image is shrunk to fit 1000×1000, turned upright, saved as WebP without its metadata,
// and given a random name. Delete old ones from cron (as the web server's user):
//   7 * * * * php /path/to/upload.php cleanup
//
// In the app: Settings → Media → Upload to: My own server → https://your-server/upload.php

declare(strict_types=1);

// ---------- settings (or override any of them in config.local.php next to this file) ----------
$CONFIG = [
    // Public web address of storage_dir. Image links are <base_url>/<days>/<k3>/<id>.webp
    'base_url' => 'https://example.com/i',
    // Where images are saved. Must be writable by PHP and served by your web server as plain
    // files at base_url. Make sure no PHP can run from it.
    'storage_dir' => __DIR__ . '/i',
    // Days to keep each image before `php upload.php cleanup` deletes it.
    'retention_days' => 7,
    // Largest upload (bytes) and image (pixels). Keep PHP's upload_max_filesize/post_max_size
    // and your web server's body size limit a little above max_bytes.
    'max_bytes' => 10 * 1024 * 1024,
    'max_pixels' => 40_000_000, // GD needs ~5 bytes a pixel, hence memory_limit below
    'memory_limit' => '320M',
    // Output size and quality.
    'max_width' => 1000,
    'max_height' => 1000,
    'webp_quality' => 82,
    'id_length' => 10,
];
if (is_file(__DIR__ . '/config.local.php')) {
    $CONFIG = array_replace($CONFIG, require __DIR__ . '/config.local.php');
}

// ---------- cleanup from the command line ----------
if (PHP_SAPI === 'cli') {
    if (($argv[1] ?? '') !== 'cleanup') {
        fwrite(STDERR, "Usage: php upload.php cleanup\n");
        exit(2);
    }
    $n = imagehost_cleanup($CONFIG);
    echo "Removed $n expired image(s)\n";
    exit(0);
}

// ---------- upload ----------
header('Content-Type: application/json; charset=utf-8');
header('Cache-Control: no-store');
header('X-Content-Type-Options: nosniff');

if (($_SERVER['REQUEST_METHOD'] ?? '') !== 'POST') {
    header('Allow: POST');
    reply(405, ['ok' => false, 'error' => 'Send the image with POST.']);
}

$file = $_FILES['image'] ?? null;
$mb = round($CONFIG['max_bytes'] / 1048576);
if (!is_array($file) || is_array($file['error'] ?? null)) {
    // PHP drops the whole body when it's over post_max_size, so a huge upload lands here too
    $tooBig = (int) ($_SERVER['CONTENT_LENGTH'] ?? 0) > $CONFIG['max_bytes'];
    reply($tooBig ? 413 : 400, ['ok' => false, 'error' => $tooBig
        ? "Images can be at most $mb MB."
        : 'No image was sent. Use a form field called "image".']);
}
switch ($file['error']) {
    case UPLOAD_ERR_OK:
        break;
    case UPLOAD_ERR_INI_SIZE:
    case UPLOAD_ERR_FORM_SIZE:
        reply(413, ['ok' => false, 'error' => "Images can be at most $mb MB."]);
    case UPLOAD_ERR_PARTIAL:
    case UPLOAD_ERR_NO_FILE:
        reply(400, ['ok' => false, 'error' => 'The upload was incomplete. Try again.']);
    default:
        error_log('imagehost: upload error ' . $file['error']);
        reply(500, ['ok' => false, 'error' => "The image couldn't be saved. Try again in a moment."]);
}
if (!is_uploaded_file($file['tmp_name'])) {
    reply(400, ['ok' => false, 'error' => 'No image was sent.']);
}

try {
    reply(200, ['ok' => true] + imagehost_store($file['tmp_name'], $CONFIG));
} catch (UploadError $e) {
    if ($e->status >= 500) {
        error_log('imagehost: ' . $e->getMessage());
    }
    reply($e->status, ['ok' => false, 'error' => $e->getMessage()]);
} catch (Throwable $e) {
    error_log('imagehost: ' . $e);
    reply(500, ['ok' => false, 'error' => "The image couldn't be processed."]);
}

// ---------- the image work ----------

final class UploadError extends RuntimeException
{
    public function __construct(string $message, public readonly int $status = 400)
    {
        parent::__construct($message);
    }
}

function reply(int $status, array $body): never
{
    http_response_code($status);
    echo json_encode($body, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
    exit;
}

/** Process and store one image. Returns ['id', 'url', 'width', 'height', 'bytes', 'expires']. */
function imagehost_store(string $src, array $cfg): array
{
    $types = [IMAGETYPE_JPEG => 'jpeg', IMAGETYPE_PNG => 'png', IMAGETYPE_WEBP => 'webp', IMAGETYPE_BMP => 'bmp'];
    $size = @filesize($src);
    if ($size === false || $size === 0) {
        throw new UploadError('The upload was empty.');
    }
    if ($size > $cfg['max_bytes']) {
        throw new UploadError('Images can be at most ' . round($cfg['max_bytes'] / 1048576) . ' MB.', 413);
    }
    // Trust the file's contents, never its name or the browser's MIME type.
    $info = @getimagesize($src);
    if ($info === false || !isset($types[$info[2]])) {
        throw new UploadError('Only JPG, PNG, WebP and BMP images can be uploaded.', 415);
    }
    [$w, $h] = $info;
    $kind = $types[$info[2]];
    if ($w < 1 || $h < 1) {
        throw new UploadError("That image couldn't be read.", 415);
    }
    if ($w * $h > $cfg['max_pixels']) {
        throw new UploadError("That image is too large ($w×$h).", 413);
    }

    @ini_set('memory_limit', $cfg['memory_limit']);
    $img = imagehost_decode($src, $kind);
    if ($kind === 'jpeg') {
        $img = imagehost_orient($img, $src);
    }
    if (!imageistruecolor($img)) {
        imagepalettetotruecolor($img);
    }
    $out = imagehost_fit($img, $cfg['max_width'], $cfg['max_height']);
    if ($out !== $img) {
        imagedestroy($img);
    }
    $ow = imagesx($out);
    $oh = imagesy($out);

    $days = max(1, (int) $cfg['retention_days']);
    [$id, $path] = imagehost_new_path($cfg, $days);
    // Write beside the final path and rename, so a half-written file is never served.
    $tmp = $path . '.part';
    $ok = imagewebp($out, $tmp, $cfg['webp_quality']);
    imagedestroy($out);
    if (!$ok || !rename($tmp, $path)) {
        @unlink($tmp);
        @unlink($path);
        throw new UploadError("The image couldn't be saved. Try again in a moment.", 500);
    }
    @chmod($path, 0644);

    return [
        'id' => $id,
        'url' => rtrim($cfg['base_url'], '/') . '/' . $days . '/' . substr($id, 0, 2) . '/' . $id . '.webp',
        'width' => $ow,
        'height' => $oh,
        'bytes' => filesize($path),
        'expires' => gmdate('Y-m-d\TH:i:s\Z', time() + $days * 86400),
    ];
}

function imagehost_decode(string $src, string $kind): GdImage
{
    $img = match ($kind) {
        'jpeg' => @imagecreatefromjpeg($src),
        'png' => @imagecreatefrompng($src),
        'webp' => @imagecreatefromwebp($src),
        'bmp' => @imagecreatefrombmp($src),
    };
    if (!$img) {
        $head = (string) @file_get_contents($src, false, null, 0, 64);
        $animated = str_contains($head, 'ANIM') || (strlen($head) > 20 && substr($head, 12, 4) === 'VP8X' && (ord($head[20]) & 0x02));
        throw new UploadError($kind === 'webp' && $animated
            ? "Animated WebP images aren't supported."
            : "That image couldn't be read. It may be damaged.", 415);
    }
    return $img;
}

// Phone photos are often stored sideways with an EXIF note saying which way is up.
function imagehost_orient(GdImage $img, string $src): GdImage
{
    if (!function_exists('exif_read_data')) {
        return $img;
    }
    $o = (int) ((@exif_read_data($src))['Orientation'] ?? 1);
    if ($o < 2 || $o > 8) {
        return $img;
    }
    if (in_array($o, [2, 4, 5, 7], true)) {
        imageflip($img, $o === 4 ? IMG_FLIP_VERTICAL : IMG_FLIP_HORIZONTAL);
    }
    // after the mirror above; imagerotate turns counter-clockwise
    $angle = match ($o) { 3 => 180, 6, 7 => 270, 5, 8 => 90, default => 0 };
    if ($angle === 0) {
        return $img;
    }
    $r = imagerotate($img, $angle, 0);
    if ($r === false) {
        return $img;
    }
    imagedestroy($img);
    return $r;
}

// Fit inside maxW × maxH keeping the aspect ratio; images already small enough are kept as is.
function imagehost_fit(GdImage $img, int $maxW, int $maxH): GdImage
{
    $w = imagesx($img);
    $h = imagesy($img);
    $scale = min(1.0, $maxW / $w, $maxH / $h);
    if ($scale >= 1.0) {
        imagesavealpha($img, true);
        return $img;
    }
    $nw = max(1, (int) round($w * $scale));
    $nh = max(1, (int) round($h * $scale));
    $dst = imagecreatetruecolor($nw, $nh);
    imagealphablending($dst, false);
    imagesavealpha($dst, true);
    imagefill($dst, 0, 0, imagecolorallocatealpha($dst, 0, 0, 0, 127));
    imagecopyresampled($dst, $img, 0, 0, 0, 0, $nw, $nh, $w, $h);
    return $dst;
}

// A random, unused id such as "k3x9Qa2mZp" and the path to store it at.
function imagehost_new_path(array $cfg, int $days): array
{
    for ($try = 0; $try < 5; $try++) {
        $id = imagehost_random_id((int) $cfg['id_length']);
        $dir = $cfg['storage_dir'] . '/' . $days . '/' . substr($id, 0, 2);
        if (!is_dir($dir) && !@mkdir($dir, 0755, true) && !is_dir($dir)) {
            throw new UploadError("The image couldn't be saved. Try again in a moment.", 500);
        }
        $path = $dir . '/' . $id . '.webp';
        // x mode fails if the name is taken, so two uploads can never share an id
        $fh = @fopen($path, 'x');
        if ($fh !== false) {
            fclose($fh);
            return [$id, $path];
        }
    }
    throw new UploadError("The image couldn't be saved. Try again in a moment.", 500);
}

function imagehost_random_id(int $len): string
{
    $alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    $id = '';
    while (strlen($id) < $len) {
        foreach (str_split(random_bytes($len)) as $b) {
            $v = ord($b);
            if ($v < 248) { // 248 = 62 × 4: drop the rest so every character is equally likely
                $id .= $alphabet[$v % 62];
                if (strlen($id) === $len) {
                    break;
                }
            }
        }
    }
    return $id;
}

/** Delete images older than their retention. Returns how many were removed. */
function imagehost_cleanup(array $cfg, ?int $now = null): int
{
    $now ??= time();
    $removed = 0;
    foreach (glob($cfg['storage_dir'] . '/*', GLOB_ONLYDIR) ?: [] as $daysDir) {
        $days = (int) basename($daysDir);
        if ($days < 1) {
            continue;
        }
        $cutoff = $now - $days * 86400;
        foreach (glob($daysDir . '/*/*') ?: [] as $f) {
            $age = @filemtime($f);
            // leftovers from a failed write are cleaned up after an hour
            $limit = str_ends_with($f, '.part') ? $now - 3600 : $cutoff;
            if ($age !== false && $age < $limit && @unlink($f)) {
                $removed++;
            }
        }
    }
    return $removed;
}
