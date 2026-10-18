# Self-hosted image host

`upload.php` is the whole image host IRChatterbox uses: one file, no libraries. Put it on any
PHP server and point the app at it to keep uploads on your own machine.

What it does with each upload:
- Accepts JPG, PNG, WebP and BMP up to 10 MB and 40 megapixels.
- Shrinks to fit 600×600 (aspect ratio kept), turns phone photos upright and strips all metadata.
- Saves as WebP under a random name and returns its link.
- Keeps it for 7 days. `php upload.php cleanup` deletes anything older.

## Setup

Needs PHP 8.1+ with the GD extension (built with WebP and BMP support), plus `exif` to turn photos upright.

1. Copy `upload.php` to your server and open its settings block at the top:
   - `base_url`: the public web address of the storage folder, e.g. `https://img.example.com/i`
   - `storage_dir`: where images are saved (default: an `i` folder next to `upload.php`). PHP
     must be able to write to it, your web server must serve it as plain files, and **PHP must
     never run inside it**.
   Or leave the file untouched and put your values in a `config.local.php` next to it:
   ```php
   <?php return ['base_url' => 'https://img.example.com/i'];
   ```
2. Allow uploads a little over 10 MB: PHP's `upload_max_filesize` and `post_max_size`, and your
   web server's request body limit (nginx `client_max_body_size`).
3. Add an hourly cron job, as the web server's user:
   ```
   7 * * * * php /path/to/upload.php cleanup
   ```
4. In IRChatterbox: **Settings → Media → Upload to: My own server**, address
   `https://your-server/upload.php`.

Rate limiting is up to your web server; the public host allows 10 uploads a minute per address.

## API

`POST /upload.php` with a multipart form field named `image`:

    200 {"ok":true,"id":"k3x9Qa2mZp","url":"https://img.example.com/i/7/k3/k3x9Qa2mZp.webp","width":600,"height":450,"bytes":38211,"expires":"2026-10-24T18:00:00Z"}
    413 {"ok":false,"error":"Images can be at most 10 MB."}
    415 {"ok":false,"error":"Only JPG, PNG, WebP and BMP images can be uploaded."}

Check GD: `php -r 'var_dump(gd_info()["WebP Support"], gd_info()["BMP Support"]);'`
