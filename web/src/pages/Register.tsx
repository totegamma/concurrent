import { Backdrop, Box, Button, CircularProgress, Divider, Paper, TextField, Typography } from '@mui/material'
import type { RJSFSchema } from '@rjsf/utils'
import Form from '@rjsf/mui'
import validator from '@rjsf/validator-ajv8'
import { useSearchParams } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { DomainProfile } from '../model'
import { useApi } from '../context/apiContext'
import ReCAPTCHA from "react-google-recaptcha";

export const Register = ({profile}: {profile: DomainProfile | null}): JSX.Element => {

    const { api } = useApi()

    const [searchParams] = useSearchParams()
    const [loading, setLoading] = useState(false);
    const [success, setSuccess] = useState(false);
    const [inviteCode, setInviteCode] = useState<string>("");
    const [captcha, setCaptcha] = useState<string>("")
    const [formData, setFormData] = useState<any>({})

    const [codeofconduct, setCodeofconduct] = useState<string | undefined>(undefined)
    const [tos, setTos] = useState<string | undefined>(undefined)
    const [schema, setSchema] = useState<RJSFSchema | undefined>(undefined)

    const encodedregistration = searchParams.get('registration')
    const registration = encodedregistration ? atob(encodedregistration.replace('-', '+').replace('_', '/')) : null
    const signature = searchParams.get('signature')
    const callback = searchParams.get('callback')
    let ccaddr = ""
    if (registration) {
        const signedObj = JSON.parse(registration)
        ccaddr = signedObj.signer
    }

    useEffect(() => {
        fetch(`/tos`, {
            method: 'GET',
        }).then(response => {
            if (response.ok) {
                return response.text()
            } else {
                throw new Error('Something went wrong')
            }
        }).then(data => {
            setTos(data)
        }).catch(error => {
            console.log(error)
        })

        fetch(`/register-template`, {
            method: 'GET',
        }).then(response => {
            if (response.ok) {
                return response.json()
            } else {
                throw new Error('Something went wrong')
            }
        }).then(data => {
            setSchema(data)
        }).catch(error => {
            console.log(error)
        })

        fetch(`/code-of-conduct`, {
            method: 'GET',
        }).then(response => {
            if (response.ok) {
                return response.text()
            } else {
                throw new Error('Something went wrong')
            }
        }).then(data => {
            setCodeofconduct(data)
        }).catch(error => {
            console.log(error)
        })

    }, [])

    console.log('registration', registration)
    console.log('signature', signature)

    const register = (meta: any): void => {
        if (!registration || !signature) return
        setLoading(true)

        api.register(registration, signature, meta, inviteCode, captcha)
        .then(async (res) => await res.json())
        .then((data) => {
            console.log(data)
            setSuccess(true)

            if (callback) {
                setTimeout(() => {
                    window.location.href = callback
                }, 1000)
            }

        }).catch((e) => {
            alert(e)
        }).finally(() => {
            setLoading(false)
        })
    }

    if (!profile || !schema) return <>Loading...</>

    return (
        <>
            <Backdrop open={loading} sx={{zIndex: 1000}}>
                <CircularProgress color="inherit" />
            </Backdrop>
            <Box
                display='flex'
                flexDirection='column'
                gap='20px'
            >
                <Box>
                    <Typography variant="h4">Registration</Typography>
                    <Typography>for {ccaddr}</Typography>
                </Box>
                <Divider />
                <Box>
                    <Typography variant="h5">{profile.nickname}</Typography>
                    <Typography>{profile.description}</Typography>
                </Box>
                <Box>
                    <Typography variant="h5">行動規範</Typography>
                    <Paper
                        variant="outlined"
                        sx={{
                            px: '20px',
                            maxHeight: '500px',
                            overflowY: 'auto',
                        }}
                    >
                        <pre
                            style={{
                                whiteSpace: 'pre-wrap',
                                wordWrap: 'break-word',
                            }}
                        >
                            {codeofconduct}
                        </pre>
                    </Paper>
                </Box>
                <Box>
                    <Typography variant="h5">利用規約およびプライバシーポリシー</Typography>
                    <Paper
                        variant="outlined"
                        sx={{
                            px: '20px',
                            maxHeight: '400px',
                            overflowY: 'auto',
                        }}
                    >
                        <pre
                            style={{
                                whiteSpace: 'pre-wrap',
                                wordWrap: 'break-word',
                            }}
                        >
                            {tos}
                        </pre>
                    </Paper>
                </Box>
            </Box>
            {profile.registration === 'close' ?
                <Typography>登録は現在受け付けていません</Typography>
            : (
            success ? <>
                <Typography>登録完了</Typography>
                {callback && <Typography>元のページに戻ります...</Typography>}
            </>
            :
                <>
                {profile.registration === 'invite' &&
                <TextField
                    label="招待コード"
                    variant="outlined"
                    value={inviteCode}
                    onChange={(e) => setInviteCode(e.target.value)}
                    sx={{my: '20px'}}
                    required
                />
                }
                <Form
                    disabled={loading}
                    schema={schema}
                    validator={validator}
                    onSubmit={(e) => {register(e.formData)}}
                    formData={formData}
                    onChange={(e) => setFormData(e.formData)}
                >
                    {profile.captchaSiteKey &&
                        <ReCAPTCHA
                          sitekey={profile.captchaSiteKey}
                          onChange={(e) => setCaptcha(e ?? '')}
                        />
                    }
                    <Button
                        type='submit'
                        variant='contained'
                        disabled={(!!profile.captchaSiteKey) && (captcha === "")}
                    >
                        Submit
                    </Button>
                </Form>
                </>

            )}
        </>
    )
}
