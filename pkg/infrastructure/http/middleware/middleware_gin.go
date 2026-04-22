package middleware

import (
	"encoding/xml"
	"io"
	"mime/multipart"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

func RegisterValidations() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterValidation("nombre", nombreValidator)
		v.RegisterValidation("xmlExtension", xmlExtensionValidator)
		v.RegisterValidation("xmlContent", xmlContentValidator)
		v.RegisterValidation("maxFileSize", maxFileSizeValidator)
	}
}

func nombreValidator(fl validator.FieldLevel) bool {
	s := fl.Field().String()
	return regexp.MustCompile(`^[A-Za-zÁÉÍÓÚÜÑáéíóúüñ]+(?:[ '-][A-Za-zÁÉÍÓÚÜÑáéíóúüñ]+)*$`).MatchString(s)
}

func xmlExtensionValidator(fl validator.FieldLevel) bool {
	file, ok := fl.Field().Interface().(multipart.FileHeader)
	if !ok {
		return false
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	return ext == ".xml"
}

func xmlContentValidator(fl validator.FieldLevel) bool {
	file, ok := fl.Field().Interface().(multipart.FileHeader)
	if !ok {
		return false
	}

	openedFile, err := file.Open()
	if err != nil {
		return false
	}
	defer openedFile.Close()

	decoder := xml.NewDecoder(openedFile)
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
	}
	return true
}

func maxFileSizeValidator(fl validator.FieldLevel) bool {
	file, ok := fl.Field().Interface().(multipart.FileHeader)
	if !ok {
		return false
	}

	return file.Size <= 10*1024*1024
}
