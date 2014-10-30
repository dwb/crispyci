package types

import (
	"net/url"
)

func (self Project) Validate() (ok bool, errors []ValidationError) {
	errors = make([]ValidationError, 0)

	if self.Name == "" {
		errors = append(errors,
			ValidationError{Field: "Name", Message: "cannot be empty"})
	}

	if self.Url == "" {
		errors = append(errors,
			ValidationError{Field: "Url", Message: "cannot be empty"})
	}

	parsedUrl, err := url.Parse(self.Url)
	if err == nil {
		if !(parsedUrl.Scheme == "http" || parsedUrl.Scheme == "https") {
			errors = append(errors,
				ValidationError{Field: "Url", Message: "must be http or https"})
		}
	} else {
		errors = append(errors,
			ValidationError{Field: "Url", Message: "is not a valid URL"})
	}

	return len(errors) == 0, errors
}
