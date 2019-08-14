package internal

import (
	"encoding/xml"
	"errors"
	"github.com/mitchellh/goamz/s3"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var NotSuccessHttpStatusError = errors.New("url returned not 200")
var NotImplementedAclMappingError = errors.New("mapping not implemented")

func tryFromUrl(u *url.URL, sourceS3Bucket *s3.Bucket) (fmeta FileMeta, err error) {
	key, err := prepareKey(u, sourceS3Bucket)
	if err != nil {
		return
	}

	resp, err := sourceS3Bucket.GetResponse(key)

	if err != nil {
		return
	}

	if resp.StatusCode != http.StatusOK {
		err = NotSuccessHttpStatusError
		return
	}

	filesize, err := strconv.ParseInt(resp.Header.Get("content-length"), 10, 0)

	if filesize == 0 {
		err = FileInvalidSizeError
		return
	}

	contentType := resp.Header.Get("content-type")
	if contentType == "" {
		err = MimeTypeNotRecognizedError
		return
	}

	acl, err := getAcl(sourceS3Bucket, key, u)

	if err != nil {
		return
	}

	fmeta.Reader = resp.Body
	fmeta.Filesize = filesize
	fmeta.Mimetype = contentType
	fmeta.Acl = acl

	return
}

func getAcl(s3Bucket *s3.Bucket, key string, u *url.URL) (acl s3.ACL, err error) {
	var cephAclResponse AccessControlPolicy
	acl = s3.Private

	params := make(map[string][]string)
	params["acl"] = []string{""}
	headers := make(map[string][]string)
	headers["Host"] = []string{s3Bucket.S3.S3BucketEndpoint}
	headers["Date"] = []string{time.Now().In(time.UTC).Format(time.RFC1123)}

	toSignString := key
	if toSignString[0:1] != "/" {
		toSignString = "/" + toSignString
	}

	toSignString = "/" + s3Bucket.Name + toSignString

	sign(s3Bucket.S3.Auth, "GET", toSignString, params, headers)

	z := s3Bucket.URL(key)
	u, _ = url.Parse(z)
	u.RawQuery = "acl"
	hreq := http.Request{
		URL:        u,
		Method:     "GET",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Close:      true,
		Header:     headers,
	}
	ok, err := s3Bucket.HTTPClient().Do(&hreq)
	if err != nil {
		return
	}

	if ok.StatusCode != http.StatusOK {
		err = NotSuccessHttpStatusError
		return
	}

	decoder := xml.NewDecoder(ok.Body)

	err = decoder.Decode(&cephAclResponse)
	if err != nil {
		return
	}

	for _, g := range cephAclResponse.AccessControlList.Grants {
		if g.Gruntee.URI == "http://acs.amazonaws.com/groups/global/AllUsers" {
			if g.Permission == "READ" {
				acl = s3.PublicRead
			}

			if g.Permission == "WRITE" {
				acl = s3.PublicReadWrite
			}
			break
		}

		if g.Gruntee.URI == "http://acs.amazonaws.com/groups/global/AuthenticatedUsers" {
			err = NotImplementedAclMappingError
		}

		if g.Gruntee.URI == "http://acs.amazonaws.com/groups/s3/LogDelivery" {
			err = NotImplementedAclMappingError
		}

		if g.Permission == "FULL_CONTROL" && g.Gruntee.URI != "" {
			err = NotImplementedAclMappingError
		}

	}

	return
}

func prepareKey(u *url.URL, bucket *s3.Bucket) (key string, err error) {
	key = u.String()
	bucketName := bucket.Name

	indexOfBucketName := strings.Index(key, bucketName)

	if indexOfBucketName == -1 {
		return
	}

	key = key[indexOfBucketName+len(bucketName)+1:]

	return
}