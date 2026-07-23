# Project: go-fitter

This project started out as a way to read .FIT files and save them into a db to be able to unify activity data
from multiple vendors into one place.

The code does two things:
- read .FIT files, flatten the data and save it in a mongodb database
- read Apple HealthKit export data, flatten it and save it in a mongodb database
- provide a simple API to query the data


## libraries and critical dependncies

Main API for processing the .FIT data is [https://github.com/muktihari/fit](https://github.com/muktihari/fit)
