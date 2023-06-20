import { useEffect, useState } from 'react';
import { Navigate, useLocation, useSearchParams } from 'react-router-dom'
import { getJWT, readEntity } from '../util';

export const Login = (): JSX.Element => {
    const [searchParams] = useSearchParams()
    const token = searchParams.get('token')

    const [loading, setLoading] = useState(true)

    useEffect(() => {
        if (!token) return
        const split = token.split('.')
        const encoded = split[1]
        const payload = window.atob(
            encoded.replace('-', '+').replace('_', '/') + '=='.slice((2 - encoded.length * 3) & 3)
        )
        const claims = JSON.parse(payload)
        const ccid = claims.iss

        readEntity(ccid).then((entity) => {
            if (!entity) {
                alert("entity not found")
                return
            }
            localStorage.setItem("ENTITY", JSON.stringify(entity))
            getJWT(token).then((jwt) => {
                if (!jwt) {
                    alert("jwt not found")
                    return
                }
                localStorage.setItem("JWT", jwt)
                setLoading(false)
            })
        })

    }, [token])

    return loading ? (
        token ? (
            <>loading...</>
        ) : (
            <>oops! something went wrong</>
        )
    ) : (
        <Navigate to='/' state={{ from: useLocation() }} replace={true} />
    )
}
