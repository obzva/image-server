# image-server

Simple image resizing server built with [Google Cloud Storage](https://cloud.google.com/storage?hl=en) and [gato](https://github.com/obzva/gato)

I built this server for learning and personal use purpose

## Features

- Resize images on the fly
- Cache processed image
  - I set processed image to be deleted after 1 day in GCS

## Usage

### Set env variables

```
GCS_BUCKET_NAME=[YOUR BUCKET NAME] # required
PORT=[PORT NUMBER SERVER SHOULD LISTEN ON] # optional, defaults to 3333
```

### API

```
GET /images/[SOME_IMAGE].[FORMAT]?w=[WIDTH]&h=[HEIGHT]
```

`FORMAT`: only jpg/jpeg and png are available
`WIDTH`, `HEIGHT`: If both dimensions are omitted, original size will be used and if only one of them omitted, aspect ratio will be kept

### Example

If you send HTTP request like this

```
GET /images/[IMAGE_NAME].[FORMAT]?w=[WIDTH]&h=[HEIGHT]
```

The server will
- Look for `processed/[IMAGE_NAME]-w[WIDTH]-h[HEIGHT].[FORMAT]` image from GCS bucket
- If found, the server will send response with this image
- If there is no cached processed image in bucket, the server will make new processed image from the original image, save it into GCS bucket and send response with it
  - newly processed image will be saved as `processed/[IMAGE_NAME]-w[WIDTH]-h[HEIGHT].[FORMAT]`
